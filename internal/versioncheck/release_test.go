package versioncheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSnapshotReturnsWhileReleaseRequestIsSlow(t *testing.T) {
	releaseGate := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseGate) }) }
	var releaseRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			releaseRequests.Add(1)
			<-releaseGate
			_, _ = w.Write([]byte(`[{"tag_name":"v0.0.1-beta.19"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	defer release()
	client := &Client{httpClient: server.Client(), apiURL: server.URL + "/releases", rawURL: server.URL + "/raw"}

	returned := make(chan bool, 1)
	go func() {
		_, _, checking := client.Snapshot(false)
		returned <- checking
	}()
	select {
	case checking := <-returned:
		if !checking {
			t.Fatal("首次查询应提示仍在检测")
		}
	case <-time.After(time.Second):
		t.Fatal("版本查询被 GitHub 请求阻塞")
	}
	if _, _, checking := client.Snapshot(false); !checking {
		t.Fatal("重复查询应复用正在运行的请求")
	}
	if got := releaseRequests.Load(); got > 1 {
		t.Fatalf("重复发起了 %d 次 GitHub 请求", got)
	}
	release()
	deadline := time.After(3 * time.Second)
	for {
		latest, err, checking := client.Snapshot(false)
		if !checking {
			if err != nil || latest.Any.PanelVersion != "0.0.1-beta.19" {
				t.Fatalf("后台查询完成后没有读到版本：%+v，错误=%v", latest, err)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("后台版本查询未完成")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestReleaseCheckUsesIndependentAgentVersionAndCache(t *testing.T) {
	var releaseRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			releaseRequests.Add(1)
			_, _ = w.Write([]byte(`[{"tag_name":"v0.0.1-beta.17"},{"tag_name":"v0.0.1"},{"tag_name":"v0.0.2-beta.1"},{"tag_name":"v1.0.0","draft":true}]`))
		case "/raw/v0.0.2-beta.1/VERSION.agent":
			_, _ = w.Write([]byte("0.0.2-beta.1\n"))
		case "/raw/v0.0.1/VERSION.agent":
			_, _ = w.Write([]byte("0.0.1\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client(), apiURL: server.URL + "/releases", rawURL: server.URL + "/raw"}
	result, err := client.Check(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Any.PanelVersion != "0.0.2-beta.1" || result.Any.AgentVersion != "0.0.2-beta.1" || result.Any.AgentError != "" ||
		result.Stable.PanelVersion != "0.0.1" || result.Stable.AgentVersion != "0.0.1" {
		t.Fatalf("版本检测结果不正确：%+v", result)
	}
	if !strings.HasSuffix(result.Any.ReleaseURL, "/v0.0.2-beta.1") || !strings.HasSuffix(result.Stable.ReleaseURL, "/v0.0.1") {
		t.Fatalf("Release 地址不正确：%+v", result)
	}
	if _, err := client.Check(context.Background(), false); err != nil || releaseRequests.Load() != 1 {
		t.Fatalf("重复请求未命中缓存：次数=%d，错误=%v", releaseRequests.Load(), err)
	}
}

func TestReleaseCheckMissingAgentFileFollowsBuildFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			_, _ = w.Write([]byte(`[{"tag_name":"v1.2.3"}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client(), apiURL: server.URL + "/releases", rawURL: server.URL + "/raw"}
	result, err := client.Check(context.Background(), false)
	if err != nil || result.Any.AgentVersion != "1.2.3" || result.Stable.AgentVersion != "1.2.3" {
		t.Fatalf("缺少 VERSION.agent 时未按构建规则回退：结果=%+v，错误=%v", result, err)
	}
}

func TestStableAgentSkipsPrereleaseVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/releases":
			_, _ = w.Write([]byte(`[{"tag_name":"v0.0.2"},{"tag_name":"v0.0.1"}]`))
		case "/raw/v0.0.2/VERSION.agent":
			_, _ = w.Write([]byte("0.0.3-beta.1"))
		case "/raw/v0.0.1/VERSION.agent":
			_, _ = w.Write([]byte("0.0.1"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &Client{httpClient: server.Client(), apiURL: server.URL + "/releases", rawURL: server.URL + "/raw"}
	result, err := client.Check(context.Background(), false)
	if err != nil || result.Stable.AgentVersion != "0.0.1" ||
		!strings.HasSuffix(result.Stable.AgentReleaseURL, "/v0.0.1") {
		t.Fatalf("正式版 Agent 不应指向 beta：结果=%+v，错误=%v", result, err)
	}
}
