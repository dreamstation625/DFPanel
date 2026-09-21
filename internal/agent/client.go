package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"dfpanel/internal/distrib"
	"dfpanel/internal/proto"
)

// Client Agent 与面板通信的客户端：HTTP 用于注册 / 心跳 / 降级轮询，WebSocket 用于实时指令
type Client struct {
	cfg  *Config
	http *http.Client
}

// NewClient 创建面板客户端
func NewClient(cfg *Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

// authQuery 生成带签名的查询串
func (c *Client) authQuery() string {
	ts := time.Now().Unix()
	return fmt.Sprintf("nodeKey=%s&ts=%d&sign=%s",
		url.QueryEscape(c.cfg.NodeKey), ts, proto.Sign(c.cfg.Secret, c.cfg.NodeKey, ts))
}

func (c *Client) api(path string) string {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	return c.cfg.PanelURL + path + sep + c.authQuery()
}

// Register 注册并上报机器信息
func (c *Client) Register(version, hostname string) error {
	var out map[string]any
	return c.post("/api/agent/register", map[string]any{
		"version":  version,
		"hostname": hostname,
		"os":       runtime.GOOS,
		"arch":     runtime.GOARCH,
		"roles":    c.cfg.Roles,
		"runtime":  c.cfg.Runtime,
	}, &out)
}

// Heartbeat 状态上报（WebSocket 不可用时的降级通道）
func (c *Client) Heartbeat(hb proto.HeartbeatData) error {
	var out map[string]any
	return c.post("/api/agent/heartbeat", hb, &out)
}

// PullCommands 拉取待执行指令（离线补发 / 轮询降级）
// 面板会把这些指令标记为已下发，属于写操作，因此统一用 POST
func (c *Client) PullCommands() ([]proto.CommandData, error) {
	var out struct {
		Commands []proto.CommandData `json:"commands"`
	}
	if err := c.post("/api/agent/commands", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return out.Commands, nil
}

// Report 上报指令执行结果（含应用失败、回滚与未确认状态）
func (c *Client) Report(commandID uint, r proto.ResultData) error {
	var out map[string]any
	return c.post("/api/agent/report", map[string]any{
		"commandId":  commandID,
		"ok":         r.OK,
		"message":    r.Message,
		"content":    r.Content,
		"running":    r.Running,
		"version":    r.Version,
		"checksum":   r.Checksum,
		"rolledBack": r.RolledBack,
		"unverified": r.Unverified,
	}, &out)
}

// DownloadBinary 从面板下载指定版本的 frps / frpc 二进制，落到版本化存储目录。
//
// goos / goarch 指定目标平台（留空取 Agent 宿主平台）。docker 运行时时必须传
// 容器平台：Windows 上的 Docker Desktop 跑的是 Linux 容器，需要 linux/amd64 的二进制。
//
// 版本号一律由面板解析：version 传 latest 时，面板在响应头 X-Frp-Version 中回传具体版本号，
// 本方法据此命名落盘文件，保证 Agent 侧永远以确定版本号记账。
// 返回 (版本化文件路径, 具体版本号, error)。
func (c *Client) DownloadBinary(kind, version, goos, goarch string) (string, string, error) {
	if kind != "frps" && kind != "frpc" {
		return "", "", fmt.Errorf("未知的二进制类型：%s", kind)
	}
	if version == "" {
		version = distrib.LatestTag
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	binDir := c.cfg.BinDir()
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return "", "", err
	}

	src := fmt.Sprintf("/downloads/%s/%s/%s/%s", kind, version, goos, goarch)

	resp, err := c.http.Get(c.api(src))
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", "", fmt.Errorf("下载 %s 失败（HTTP %d）：%s", kind, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// 面板回传解析后的具体版本号，避免 Agent 侧出现 latest 这种不确定值
	resolved := strings.TrimSpace(resp.Header.Get("X-Frp-Version"))
	if resolved == "" || !distrib.ValidVersion(resolved) {
		resolved = version
	}
	if !distrib.ValidVersion(resolved) {
		return "", "", fmt.Errorf("面板未返回可识别的版本号（%s）", version)
	}

	dest := filepath.Join(binDir, distrib.BinaryName(kind, resolved, goos, goarch))
	tmp := dest + ".tmp"
	_ = os.Remove(tmp)

	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", "", err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", "", err
	}
	_ = os.Chmod(dest, 0o755)

	// 落盘后自检：只在能跑起来（同平台）时才校验，跨平台下载（如 Windows 宿主取 linux 二进制）
	// 无法执行，此处自动跳过，版本正确性交由容器启动后的日志与健康探测确认。
	if got, err := distrib.QueryVersion(dest); err == nil && got != resolved {
		_ = os.Remove(dest)
		return "", "", fmt.Errorf("下载的二进制版本与期望不一致：期望 %s，实际 %s", resolved, got)
	}
	return dest, resolved, nil
}

// DialWS 建立到面板的 WebSocket 反向长连接
func (c *Client) DialWS() (*websocket.Conn, error) {
	base := c.cfg.PanelURL
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	default:
		base = "ws://" + base
	}
	dialer := &websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(base+"/api/agent/ws?"+c.authQuery(), nil)
	return conn, err
}

func (c *Client) get(path string, out any) error {
	resp, err := c.http.Get(c.api(path))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp, out)
}

func (c *Client) post(path string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, c.api(path), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decode(resp, out)
}

func decode(resp *http.Response, out any) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("面板返回 %d：%s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}
