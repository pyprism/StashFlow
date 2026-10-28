package web

import (
	"net"
	"net/http"
	"os"
	"runtime/pprof"

	"github.com/gin-gonic/gin"
)

// Debug routes are opt-in and restricted to direct loopback connections.
// Forwarded headers are deliberately ignored.
func (s *Server) registerDebugRoutes(r *gin.Engine) {
	if os.Getenv("STASHFLOW_DEBUG") != "1" {
		return
	}
	debug := r.Group("/debug", func(c *gin.Context) {
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Next()
	})
	debug.GET("/diagnostics", func(c *gin.Context) { c.JSON(http.StatusOK, s.mgr.Diagnostics()) })
	for _, name := range []string{"heap", "goroutine"} {
		debug.GET("/pprof/"+name, func(c *gin.Context) {
			c.Header("Content-Type", "application/octet-stream")
			_ = pprof.Lookup(name).WriteTo(c.Writer, 0)
		})
	}
}
