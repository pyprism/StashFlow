package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"stashflow/internal/app"
	"stashflow/internal/config"
)

const daemonEnv = "_STASHFLOW_DAEMON"

var version = "v1.0.0"

func main() {
	// If we are the spawned daemon child, run the server directly.
	// This must be checked first — the child is launched without args.
	if os.Getenv(daemonEnv) == "1" {
		if err := runDaemon(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "start":
		foreground := false
		for _, arg := range os.Args[2:] {
			if arg == "--foreground" || arg == "-f" {
				foreground = true
			}
		}
		if err := runStart(foreground); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "stop":
		if err := runStop(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "status":
		if err := runStatus(); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println("StashFlow " + version)
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("StashFlow - storage aware torrent client")
	fmt.Println()
	fmt.Println("Usage: stashflow <command> [flags]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  start            Start the daemon (daemonizes if configured)")
	fmt.Println("  start -f         Start in foreground (don't daemonize)")
	fmt.Println("  stop             Stop a running daemon")
	fmt.Println("  status           Show daemon and queue status")
	fmt.Println("  version          Print version")
}

// runStart is the parent process entry point.
func runStart(foreground bool) error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}

	cfg, cfgPath, err := config.Load()
	if err != nil {
		return err
	}

	// If not configured yet, prompt interactively then save.
	if cfg.StoragePath == "" || cfg.Port == 0 {
		if err := promptInitialConfig(cfg); err != nil {
			return err
		}
		if err := config.SaveToPath(cfg, cfgPath); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(cfg.StoragePath, 0o755); err != nil {
		return err
	}

	// Check if already running.
	pidPath, err := config.PidPath()
	if err != nil {
		return err
	}
	if pid, alive := readAlivePid(pidPath); alive {
		return fmt.Errorf("already running (pid %d)", pid)
	}

	if foreground {
		return runForeground(cfg, cfgPath, pidPath)
	}

	return spawnDaemon(cfg)
}

// spawnDaemon re-executes the current binary as a background process.
func spawnDaemon(cfg *config.Config) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	logPath, err := config.LogPath()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}

	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), daemonEnv+"=1")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// Detach from the parent process group.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("spawn daemon: %w", err)
	}

	// The child writes its own PID file; we just report.
	fmt.Printf("StashFlow started (pid %d)\n", cmd.Process.Pid)
	fmt.Printf("  http://localhost:%d\n", cfg.Port)
	fmt.Printf("  log: %s\n", logPath)

	// Detach: don't wait for child.
	_ = cmd.Process.Release()
	logFile.Close()
	return nil
}

// runDaemon is the daemon child entry point (invoked via _STASHFLOW_DAEMON=1).
func runDaemon() error {
	if err := config.EnsureDirs(); err != nil {
		return err
	}

	cfg, cfgPath, err := config.Load()
	if err != nil {
		return err
	}

	if cfg.StoragePath == "" || cfg.Port == 0 {
		return errors.New("not configured; run 'stashflow start' interactively first")
	}

	if err := os.MkdirAll(cfg.StoragePath, 0o755); err != nil {
		return err
	}

	pidPath, err := config.PidPath()
	if err != nil {
		return err
	}

	return runForeground(cfg, cfgPath, pidPath)
}

// runForeground runs the server in the current process (shared by daemon child
// and explicit --foreground mode).
func runForeground(cfg *config.Config, cfgPath, pidPath string) error {
	statePath, err := config.StatePath()
	if err != nil {
		return err
	}
	torrentDir, err := config.TorrentDir()
	if err != nil {
		return err
	}

	if err := writePid(pidPath); err != nil {
		return err
	}
	defer os.Remove(pidPath)

	appInstance, err := app.New(cfg, cfgPath, statePath, torrentDir)
	if err != nil {
		return err
	}

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-stopCh
		_ = app.ShutdownWithTimeout(appInstance, 10*time.Second)
	}()

	fmt.Printf("StashFlow running at http://localhost:%d (pid %d)\n", cfg.Port, os.Getpid())
	err = appInstance.Run()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func runStop() error {
	pidPath, err := config.PidPath()
	if err != nil {
		return err
	}
	pid, alive := readAlivePid(pidPath)
	if !alive {
		// Clean up stale PID file if it exists.
		_ = os.Remove(pidPath)
		return errors.New("StashFlow is not running")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to stop (pid %d): %w", pid, err)
	}
	// Wait briefly for the process to exit and clean up.
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			break
		}
	}
	_ = os.Remove(pidPath)
	fmt.Printf("StashFlow stopped (pid %d)\n", pid)
	return nil
}

func runStatus() error {
	cfg, _, err := config.Load()
	if err != nil {
		return err
	}
	statePath, err := config.StatePath()
	if err != nil {
		return err
	}
	pidPath, err := config.PidPath()
	if err != nil {
		return err
	}

	running := "stopped"
	if pid, alive := readAlivePid(pidPath); alive {
		running = fmt.Sprintf("running (pid %d)", pid)
	} else {
		// Clean up stale PID file.
		_ = os.Remove(pidPath)
	}

	var state struct {
		Items []struct {
			Status string `json:"status"`
		} `json:"items"`
	}
	if stateBytes, err := os.ReadFile(statePath); err == nil {
		if err := json.Unmarshal(stateBytes, &state); err != nil {
			return fmt.Errorf("invalid state file")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unable to read state: %w", err)
	}

	totals := map[string]int{}
	for _, item := range state.Items {
		totals[item.Status]++
	}

	fmt.Println("StashFlow status")
	fmt.Printf("  Status:      %s\n", running)
	fmt.Printf("  Storage:     %s\n", cfg.StoragePath)
	fmt.Printf("  Port:        %d\n", cfg.Port)
	fmt.Printf("  Queue:       %d\n", totals["queued"])
	fmt.Printf("  Downloading: %d\n", totals["downloading"])
	fmt.Printf("  Completed:   %d\n", totals["completed"])
	fmt.Printf("  Errors:      %d\n", totals["error"])
	return nil
}

// readAlivePid reads a PID file and checks whether that process is still running.
func readAlivePid(pidPath string) (int, bool) {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	// Signal 0 doesn't send anything but checks if the process exists.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return pid, false
	}
	return pid, true
}

func writePid(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func promptInitialConfig(cfg *config.Config) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Storage path: ")
	storage, _ := reader.ReadString('\n')
	storage = strings.TrimSpace(storage)
	if storage == "" {
		return errors.New("storage path is required")
	}
	fmt.Print("Port (default 8080): ")
	portStr, _ := reader.ReadString('\n')
	portStr = strings.TrimSpace(portStr)
	port := 8080
	if portStr != "" {
		val, err := strconv.Atoi(portStr)
		if err != nil {
			return errors.New("invalid port")
		}
		port = val
	}
	cfg.StoragePath = storage
	cfg.Port = port
	if cfg.MaxUsagePercent == 0 {
		cfg.MaxUsagePercent = 0.90
	}
	return nil
}
