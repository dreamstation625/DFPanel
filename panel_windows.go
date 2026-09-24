//go:build windows

package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"

	"dfpanel/internal/config"
)

const panelServiceName = "DFPanel"

// servePanel 自动识别是否由 Windows 服务控制管理器启动，同一个程序仍可在控制台直接运行。
func servePanel(server *http.Server, cfg *config.Config) error {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return err
	}
	if !isService {
		return server.ListenAndServe()
	}

	handler := &panelService{server: server}
	if err := svc.Run(panelServiceName, handler); err != nil {
		return err
	}
	return handler.err
}

// preparePanelLog 在初始化数据库前打开日志，确保服务启动失败也留下诊断信息。
func preparePanelLog(cfg *config.Config) (func(), error) {
	isService, err := svc.IsWindowsService()
	if err != nil {
		return nil, err
	}
	if !isService {
		return func() {}, nil
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(cfg.DataDir, "panel.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	log.SetOutput(logFile)
	return func() { _ = logFile.Close() }, nil
}

type panelService struct {
	server *http.Server
	err    error
}

func (s *panelService) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending}
	listener, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		s.err = err
		return false, 1
	}
	done := make(chan error, 1)
	go func() { done <- s.server.Serve(listener) }()

	status := svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	changes <- status
	for {
		select {
		case request := <-requests:
			switch request.Cmd {
			case svc.Interrogate:
				changes <- status
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				if err := s.server.Shutdown(ctx); err != nil {
					log.Printf("等待面板停止失败：%v", err)
					_ = s.server.Close()
				}
				cancel()
				if err := <-done; err != nil && !errors.Is(err, http.ErrServerClosed) {
					s.err = err
					return false, 1
				}
				return false, 0
			}
		case err := <-done:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				s.err = err
				return false, 1
			}
			return false, 0
		}
	}
}
