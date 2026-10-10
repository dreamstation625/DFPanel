package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"dfpanel/internal/config"
	"dfpanel/internal/versioncheck"
)

type releaseCheckTransport func(*http.Request) (*http.Response, error)

func (transport releaseCheckTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return transport(req)
}

func TestProgramVersionReturnsReleaseWithoutWaitingForGitHub(t *testing.T) {
	securityDB(t)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	previousTransport, previousClient := http.DefaultTransport, versioncheck.Default
	http.DefaultTransport = releaseCheckTransport(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/releases") {
			select {
			case <-gate:
			case <-req.Context().Done():
				return nil, req.Context().Err()
			}
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`[{"tag_name":"v0.0.3-beta.1","body":"预发布说明"},{"tag_name":"v0.0.2","body":"正式版说明"}]`))}, nil
		}
		return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: http.NoBody}, nil
	})
	versioncheck.Default = versioncheck.NewClient()
	t.Cleanup(func() { http.DefaultTransport, versioncheck.Default = previousTransport, previousClient })
	r := ginRouter()
	r.GET("/stable", ProgramVersionCheck(&config.Config{Version: "0.0.1"}))
	r.GET("/pre", ProgramVersionCheck(&config.Config{Version: "0.0.1-beta.1"}))
	query := func(path string) struct {
		Panel    programVersionStatus `json:"panel"`
		Checking bool                 `json:"checking"`
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		var result struct {
			Panel    programVersionStatus `json:"panel"`
			Checking bool                 `json:"checking"`
		}
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil {
			t.Fatalf("版本检测响应异常：%d %s", w.Code, w.Body.String())
		}
		return result
	}
	// 上游尚未返回时，页面请求立即得到 checking，不占用页面的超时时间。
	started := time.Now()
	if result := query("/stable"); !result.Checking || time.Since(started) > time.Second {
		t.Fatalf("检测接口被上游阻塞：%+v", result)
	}
	release()
	deadline := time.Now().Add(3 * time.Second)
	for {
		result := query("/stable")
		if !result.Checking {
			if result.Panel.Release == nil || result.Panel.Release.Body != "正式版说明" || result.Panel.Latest != "0.0.2" {
				t.Fatalf("正式版接口未返回对应说明：%+v", result.Panel)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("后台查询没有完成")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if result := query("/pre"); result.Panel.Release == nil || result.Panel.Release.Body != "预发布说明" || result.Panel.Latest != "0.0.3-beta.1" {
		t.Fatalf("预发布接口未返回对应说明：%+v", result.Panel)
	}
}

func TestPublishedForStableAndPrerelease(t *testing.T) {
	latest := versioncheck.Latest{
		Any:    versioncheck.Published{PanelVersion: "0.0.2-beta.1", AgentVersion: "0.0.2-beta.1", Release: &versioncheck.ReleaseInfo{Body: "预发布说明"}},
		Stable: versioncheck.Published{PanelVersion: "0.0.1", AgentVersion: "0.0.1", Release: &versioncheck.ReleaseInfo{Body: "正式版说明"}},
	}
	stable := publishedFor("0.0.1", latest)
	if stable.PanelVersion != "0.0.1" || compareProgramVersion("0.0.1", stable.PanelVersion, "", "").UpdateAvailable {
		t.Fatalf("正式版不应提示 beta 更新：%+v", stable)
	}
	withoutStable := publishedFor("0.0.1", versioncheck.Latest{Any: latest.Any})
	if compareProgramVersion("0.0.1", withoutStable.PanelVersion, "", "").UpdateAvailable {
		t.Fatal("只有 beta Release 时，正式版也不应提示更新")
	}
	pre := publishedFor("0.0.1-beta.17", latest)
	if pre.PanelVersion != "0.0.2-beta.1" || !compareProgramVersion("0.0.1-beta.17", pre.PanelVersion, "", "").UpdateAvailable {
		t.Fatalf("预发布版应检测后续 beta 更新：%+v", pre)
	}
	if stable.Release.Body != "正式版说明" || pre.Release.Body != "预发布说明" || withoutStable.Release != nil {
		t.Fatal("Release 说明必须遵循运行版本的发布通道")
	}
}

func TestCompareProgramVersion(t *testing.T) {
	tests := []struct {
		current, latest, checkError string
		state                       string
		update                      bool
	}{
		{"0.0.1-beta.17", "0.0.1-beta.18", "", "update", true},
		{"0.0.1-beta.17", "0.0.1-beta.17", "", "current", false},
		{"0.0.1-beta.18", "0.0.1-beta.17", "", "ahead", false},
		{"dev", "0.0.1-beta.18", "", "unknown", false},
		{"0.0.1-beta.17", "", "GitHub 不可达", "error", false},
	}
	for _, test := range tests {
		status := compareProgramVersion(test.current, test.latest, "https://example.test/release", test.checkError)
		if status.State != test.state || status.UpdateAvailable != test.update {
			t.Errorf("%q 对 %q：结果=%+v，期望状态=%s，可更新=%t", test.current, test.latest, status, test.state, test.update)
		}
	}
}
