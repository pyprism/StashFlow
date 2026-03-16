package web

import (
	"embed"
	"encoding/json"
	"io"
	"net/http"
	stdpath "path"

	"github.com/gin-gonic/gin"

	"stashflow/internal/config"
	"stashflow/internal/storage"
	"stashflow/internal/torrent"
)

//go:embed all:ui
var webFS embed.FS

type Server struct {
	cfg     *config.Config
	cfgPath string
	mgr     *torrent.Manager
	hub     *Hub
}

func NewServer(cfg *config.Config, cfgPath string, mgr *torrent.Manager, hub *Hub) *Server {
	return &Server{cfg: cfg, cfgPath: cfgPath, mgr: mgr, hub: hub}
}

func (s *Server) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/api/state", s.handleState)
	r.GET("/api/settings", s.handleGetSettings)
	r.PUT("/api/settings", s.handleUpdateSettings)
	r.POST("/api/torrents", s.handleAddTorrent)
	r.DELETE("/api/torrents/:id", s.handleRemoveTorrent)
	r.POST("/api/queue/reorder", s.handleReorder)
	r.POST("/api/storage/check", s.handleStorageCheck)
	r.GET("/api/events", s.handleEvents)

	r.GET("/", s.handleIndex)
	r.GET("/assets/*filepath", s.handleAssets)

	return r
}

func (s *Server) handleIndex(c *gin.Context) {
	data, err := webFS.ReadFile("ui/index.html")
	if err != nil {
		c.String(http.StatusInternalServerError, "failed to load UI")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}

func (s *Server) handleAssets(c *gin.Context) {
	fp := c.Param("filepath")
	if fp == "" || fp == "/" {
		c.Status(http.StatusNotFound)
		return
	}
	// embed.FS always uses forward slashes; use path (not filepath) for cleaning
	clean := stdpath.Clean(fp)
	name := "ui" + clean
	data, err := webFS.ReadFile(name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	contentType := "text/plain; charset=utf-8"
	switch stdpath.Ext(clean) {
	case ".css":
		contentType = "text/css; charset=utf-8"
	case ".js":
		contentType = "application/javascript; charset=utf-8"
	case ".svg":
		contentType = "image/svg+xml"
	case ".png":
		contentType = "image/png"
	}
	c.Data(http.StatusOK, contentType, data)
}

func (s *Server) handleState(c *gin.Context) {
	stats, _ := storage.Stat(s.cfg.StoragePath, s.cfg.MaxUsagePercent)
	c.JSON(http.StatusOK, gin.H{
		"state":   s.mgr.State(),
		"storage": stats,
	})
}

func (s *Server) handleGetSettings(c *gin.Context) {
	c.JSON(http.StatusOK, s.cfg)
}

func (s *Server) handleUpdateSettings(c *gin.Context) {
	var payload config.Config
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}

	restartRequired := false
	if payload.Port != 0 && payload.Port != s.cfg.Port {
		restartRequired = true
		s.cfg.Port = payload.Port
	}
	if payload.StoragePath != "" && payload.StoragePath != s.cfg.StoragePath {
		restartRequired = true
		s.cfg.StoragePath = payload.StoragePath
	}
	if payload.MaxUsagePercent > 0 {
		if payload.MaxUsagePercent < 0.01 || payload.MaxUsagePercent > 1.0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "max usage percent must be between 1% and 100%"})
			return
		}
		if payload.MaxUsagePercent != s.cfg.MaxUsagePercent {
			s.cfg.MaxUsagePercent = payload.MaxUsagePercent
			s.mgr.SetMaxUsagePercent(payload.MaxUsagePercent)
		}
	}

	if err := config.SaveToPath(s.cfg, s.cfgPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save settings"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"restartRequired": restartRequired})
}

func (s *Server) handleAddTorrent(c *gin.Context) {
	magnet := c.PostForm("magnet")
	if magnet != "" {
		item, err := s.mgr.AddMagnet(magnet)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, item)
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing torrent file or magnet"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read file"})
		return
	}

	item, err := s.mgr.AddTorrentFile(header.Filename, data)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, item)
}

func (s *Server) handleRemoveTorrent(c *gin.Context) {
	id := c.Param("id")
	if err := s.mgr.Remove(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *Server) handleReorder(c *gin.Context) {
	var payload struct {
		Order []string `json:"order"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid payload"})
		return
	}
	s.mgr.Reorder(payload.Order)
	c.Status(http.StatusNoContent)
}

func (s *Server) handleStorageCheck(c *gin.Context) {
	s.mgr.CheckAndStartNext()
	stats, _ := storage.Stat(s.cfg.StoragePath, s.cfg.MaxUsagePercent)
	c.JSON(http.StatusOK, stats)
}

func (s *Server) handleEvents(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Writer.Flush()

	ch := s.hub.Subscribe()
	defer s.hub.Unsubscribe(ch)

	// Send initial state
	stats, _ := storage.Stat(s.cfg.StoragePath, s.cfg.MaxUsagePercent)
	initial := gin.H{"state": s.mgr.State(), "storage": stats}
	data, _ := json.Marshal(initial)
	writeSSE(c, data)

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case msg := <-ch:
			writeSSE(c, msg)
		}
	}
}

func writeSSE(c *gin.Context, data []byte) {
	_, _ = c.Writer.Write([]byte("data: "))
	_, _ = c.Writer.Write(data)
	_, _ = c.Writer.Write([]byte("\n\n"))
	c.Writer.Flush()
}

// no-op: using encoding/json directly
