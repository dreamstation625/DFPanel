//go:build !windows

package main

import (
	"net/http"

	"dfpanel/internal/config"
)

// servePanel 在非 Windows 平台以前台进程运行，由 systemd 或容器负责守护。
func servePanel(server *http.Server, _ *config.Config) error {
	return server.ListenAndServe()
}

func preparePanelLog(_ *config.Config) (func(), error) {
	return func() {}, nil
}
