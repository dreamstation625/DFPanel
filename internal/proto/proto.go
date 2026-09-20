// Package proto 定义面板与 Agent 之间的通信协议，被双方共同引用。
// 该包刻意保持零依赖（仅标准库），以便 Agent 二进制不引入 gorm / sqlite 等面板侧组件。
package proto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// 消息类型
const (
	MsgCommand       = "command"        // 面板 -> Agent：下发指令
	MsgCommandResult = "command_result" // Agent -> 面板：指令执行结果
	MsgHeartbeat     = "heartbeat"      // Agent -> 面板：状态上报
	MsgPing          = "ping"           // 面板 -> Agent：保活探测
	MsgPong          = "pong"           // Agent -> 面板：保活应答
)

// 指令类型
const (
	CmdApply    = "apply"
	CmdStart    = "start"
	CmdStop     = "stop"
	CmdRestart  = "restart"
	CmdLog      = "log"
	CmdRollback = "rollback" // 回滚到指定历史版本（payload: {"targetVersion":N}）
	CmdVersions = "versions" // 列出 Agent 本地保存的历史配置版本
)

// 目标类型
const (
	TargetServer = "server" // frps 服务端
	TargetNode   = "node"   // frpc 节点
)

// DefaultSkew 允许的时钟偏移
const DefaultSkew = 5 * time.Minute

// Envelope 统一消息信封
type Envelope struct {
	Type string          `json:"type"`
	ID   string          `json:"id,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
}

// CommandData 面板下发的指令体
type CommandData struct {
	CommandID  uint   `json:"commandId"`
	Type       string `json:"type"`
	TargetType string `json:"targetType"`
	TargetID   uint   `json:"targetId"`
	Payload    string `json:"payload,omitempty"`
	Version    int    `json:"version"`
}

// TargetState 单个托管对象（frps / frpc）的运行态
type TargetState struct {
	TargetType string `json:"targetType"`
	TargetID   uint   `json:"targetId"`
	Running    bool   `json:"running"`
	Version    int    `json:"version"`
	Checksum   string `json:"checksum"`
	LastError  string `json:"lastError"`
}

// HeartbeatData 心跳上报
type HeartbeatData struct {
	Version  string        `json:"version"`
	Hostname string        `json:"hostname"`
	Targets  []TargetState `json:"targets"`
}

// HistoryEntry Agent 本地保存的一份历史配置
type HistoryEntry struct {
	Version  int    `json:"version"`
	Size     int64  `json:"size"`
	Time     int64  `json:"time"` // unix 秒
	Checksum string `json:"checksum"`
	Current  bool   `json:"current"` // 是否与当前生效配置一致
}

// ResultData 指令执行结果
type ResultData struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	Content    string `json:"content,omitempty"`
	Running    bool   `json:"running,omitempty"`
	Version    int    `json:"version,omitempty"`
	Checksum   string `json:"checksum,omitempty"`
	RolledBack bool   `json:"rolledBack,omitempty"`
	// Unverified 配置已写入并重启，但未在观察窗口内确认连通（不视为失败，不回滚）
	Unverified bool `json:"unverified,omitempty"`
	// Versions CmdVersions 指令返回的历史版本列表
	Versions []HistoryEntry `json:"versions,omitempty"`
}

// Sign 生成请求签名：HMAC-SHA256(secret, "nodeKey.ts")
func Sign(secret, nodeKey string, ts int64) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(fmt.Sprintf("%s.%d", nodeKey, ts)))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify 校验签名与时间戳，防止重放与伪造
func Verify(secret, nodeKey string, ts int64, sign string, skew time.Duration) error {
	if secret == "" || nodeKey == "" || sign == "" {
		return errors.New("缺少鉴权参数")
	}
	if skew <= 0 {
		skew = DefaultSkew
	}
	if delta := time.Since(time.Unix(ts, 0)); delta > skew || delta < -skew {
		return errors.New("时间戳超出允许范围，请检查机器时间")
	}
	if !hmac.Equal([]byte(Sign(secret, nodeKey, ts)), []byte(sign)) {
		return errors.New("签名校验失败")
	}
	return nil
}
