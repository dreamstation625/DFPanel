package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallationIdentityPersistsPerDataDirectory(t *testing.T) {
	for _, name := range []string{"DFPANEL_URL", "DFPANEL_NODE_KEY", "DFPANEL_NODE_SECRET", "DFPANEL_ROLES", "DFPANEL_DATA_DIR"} {
		t.Setenv(name, "")
	}
	makeConfig := func(dir string) string {
		t.Helper()
		path := filepath.Join(dir, "agent.json")
		content, _ := json.Marshal(Config{PanelURL: "http://panel", NodeKey: "same-key", Secret: "same-secret", Roles: "frpc", DataDir: filepath.Join(dir, "data")})
		if err := os.WriteFile(path, content, 0o600); err != nil { t.Fatal(err) }
		return path
	}
	firstPath := makeConfig(t.TempDir())
	otherPath := makeConfig(t.TempDir())
	first, err := LoadConfig(firstPath)
	if err != nil { t.Fatal(err) }
	restarted, err := LoadConfig(firstPath)
	if err != nil { t.Fatal(err) }
	other, err := LoadConfig(otherPath)
	if err != nil { t.Fatal(err) }
	if len(first.InstanceID) != 32 || first.InstanceID != restarted.InstanceID || first.InstanceID == other.InstanceID {
		t.Fatalf("安装实例标识未按数据目录持久化：%q %q %q", first.InstanceID, restarted.InstanceID, other.InstanceID)
	}
	if first.HostID == "" || first.HostID != other.HostID {
		t.Fatal("同机不同 Agent 应上报相同主机标识")
	}
}
