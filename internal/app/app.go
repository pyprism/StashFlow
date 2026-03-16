package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"stashflow/internal/config"
	"stashflow/internal/storage"
	"stashflow/internal/torrent"
	"stashflow/internal/web"
)

type App struct {
	cfg     *config.Config
	cfgPath string
	mgr     *torrent.Manager
	hub     *web.Hub
	server  *http.Server
}

func New(cfg *config.Config, cfgPath, statePath, torrentDir string) (*App, error) {
	mgr, err := torrent.NewManager(cfg.StoragePath, torrentDir, statePath, cfg.MaxUsagePercent)
	if err != nil {
		return nil, err
	}
	hub := web.NewHub()
	mgr.SetOnChange(func() {
		stats, _ := storageSnapshot(cfg)
		hub.Broadcast(map[string]any{
			"state":   mgr.State(),
			"storage": stats,
		})
	})

	srv := web.NewServer(cfg, cfgPath, mgr, hub)
	httpSrv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: srv.Router(),
	}

	app := &App{cfg: cfg, cfgPath: cfgPath, mgr: mgr, hub: hub, server: httpSrv}
	return app, nil
}

func (a *App) Run() error {
	a.mgr.StartBackground()
	return a.server.ListenAndServe()
}

func (a *App) Shutdown(ctx context.Context) error {
	_ = a.mgr.Close()
	return a.server.Shutdown(ctx)
}

func storageSnapshot(cfg *config.Config) (any, error) {
	stats, err := storage.Stat(cfg.StoragePath, cfg.MaxUsagePercent)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

func ShutdownWithTimeout(a *App, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return a.Shutdown(ctx)
}
