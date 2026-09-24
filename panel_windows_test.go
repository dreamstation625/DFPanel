//go:build windows

package main

import (
	"net/http"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
)

func TestPanelServiceStartsAndStops(t *testing.T) {
	service := &panelService{server: &http.Server{Addr: "127.0.0.1:0", Handler: http.NotFoundHandler()}}
	requests := make(chan svc.ChangeRequest, 1)
	changes := make(chan svc.Status, 4)
	done := make(chan uint32, 1)
	go func() {
		_, code := service.Execute(nil, requests, changes)
		done <- code
	}()

	select {
	case status := <-changes:
		if status.State != svc.StartPending {
			t.Fatalf("首次状态 = %v，期望 StartPending", status.State)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("服务未报告启动中")
	}
	select {
	case status := <-changes:
		if status.State != svc.Running {
			t.Fatalf("启动状态 = %v，期望 Running", status.State)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("服务未报告运行中")
	}

	requests <- svc.ChangeRequest{Cmd: svc.Stop}
	select {
	case code := <-done:
		if code != 0 || service.err != nil {
			t.Fatalf("停止状态码 = %d，错误 = %v", code, service.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("服务未能停止")
	}
}
