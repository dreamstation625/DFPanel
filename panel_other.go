//go:build !windows

package main

import (
	"dfpanel/internal/logutil"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"dfpanel/internal/config"
)

// servePanel 在非 Windows 平台以前台进程运行，由 systemd 或容器负责守护。
func servePanel(server *http.Server, _ *config.Config) error {
	return server.ListenAndServe()
}

func preparePanelLog(cfg *config.Config) (func(), error) {
	f, err := logutil.Open(filepath.Join(cfg.DataDir, "panel.log"))
	if err != nil {
		return nil, err
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	return func() { f.Close() }, nil
}
