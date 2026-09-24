package handler

import (
	"testing"

	"dfpanel/internal/versioncheck"
)

func TestPublishedForStableAndPrerelease(t *testing.T) {
	latest := versioncheck.Latest{
		Any:    versioncheck.Published{PanelVersion: "0.0.2-beta.1", AgentVersion: "0.0.2-beta.1"},
		Stable: versioncheck.Published{PanelVersion: "0.0.1", AgentVersion: "0.0.1"},
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
