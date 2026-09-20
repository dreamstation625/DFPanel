// Package agenthub 实现面板与远端 Agent 之间的实时通道。
//
// 拓扑：Agent 主动反向连接面板（WebSocket），面板不主动外连，
// 因此 Agent 可以部署在任意内网 / NAT 之后的机器上，面板只需要有一个可达地址。
// Agent 离线或 WebSocket 不可用时，退化为 HTTP 轮询（见 internal/handler/agent.go）。
package agenthub

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"dfpanel/internal/database"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
)

// Hub 管理所有在线 Agent 连接
type Hub struct {
	mu      sync.RWMutex
	conns   map[uint]*Conn
	pending map[string]chan proto.Envelope
	states  map[uint]map[string]proto.TargetState
}

// New 创建 Hub
func New() *Hub {
	return &Hub{
		conns:   make(map[uint]*Conn),
		pending: make(map[string]chan proto.Envelope),
		states:  make(map[uint]map[string]proto.TargetState),
	}
}

// ErrOffline Agent 当前不在线（调用方应把指令排队，等待轮询补发）
var ErrOffline = errors.New("agent 离线")

// Conn 单条 Agent 长连接
type Conn struct {
	hub     *Hub
	agentID uint
	ws      *websocket.Conn
	send    chan proto.Envelope
	done    chan struct{}
	once    sync.Once
}

// Attach 把一个已鉴权的 WebSocket 连接接入 Hub，并启动读写协程
func (h *Hub) Attach(agentID uint, ws *websocket.Conn) *Conn {
	c := &Conn{
		hub:     h,
		agentID: agentID,
		ws:      ws,
		send:    make(chan proto.Envelope, 32),
		done:    make(chan struct{}),
	}

	h.mu.Lock()
	if old, ok := h.conns[agentID]; ok {
		old.close()
	}
	h.conns[agentID] = c
	h.mu.Unlock()

	go c.writeLoop()
	go c.readLoop()
	return c
}

// Wait 阻塞直到连接关闭（HTTP handler 中调用以保持连接存活）
func (c *Conn) Wait() {
	<-c.done
}

// Online 判断 Agent 是否在线
func (h *Hub) Online(agentID uint) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.conns[agentID]
	return ok
}

// AgentIDs 返回当前在线的 Agent ID 列表
func (h *Hub) AgentIDs() []uint {
	h.mu.RLock()
	defer h.mu.RUnlock()
	ids := make([]uint, 0, len(h.conns))
	for id := range h.conns {
		ids = append(ids, id)
	}
	return ids
}

// Send 向指定 Agent 发送指令并同步等待结果；Agent 离线返回 ErrOffline
func (h *Hub) Send(agentID uint, env proto.Envelope, timeout time.Duration) (proto.Envelope, error) {
	h.mu.RLock()
	c, ok := h.conns[agentID]
	h.mu.RUnlock()
	if !ok {
		return proto.Envelope{}, ErrOffline
	}

	if env.ID == "" {
		env.ID = fmt.Sprintf("%d-%d", time.Now().UnixNano(), agentID)
	}
	ch := make(chan proto.Envelope, 1)

	h.mu.Lock()
	h.pending[env.ID] = ch
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, env.ID)
		h.mu.Unlock()
	}()

	select {
	case c.send <- env:
	case <-time.After(5 * time.Second):
		return proto.Envelope{}, errors.New("发送指令超时")
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return proto.Envelope{}, errors.New("等待 Agent 响应超时")
	}
}

// UpdateState 更新 Agent 上报的运行态，并刷新 Agent 在线信息
func (h *Hub) UpdateState(agentID uint, hb proto.HeartbeatData, remoteAddr string) {
	now := time.Now()

	h.mu.Lock()
	st := make(map[string]proto.TargetState, len(hb.Targets))
	for _, t := range hb.Targets {
		st[stateKey(t.TargetType, t.TargetID)] = t
	}
	h.states[agentID] = st
	h.mu.Unlock()

	updates := map[string]any{
		"status":      "online",
		"last_seen":   now,
		"remote_addr": remoteAddr,
		"last_error":  "",
	}
	if hb.Version != "" {
		updates["version"] = hb.Version
	}
	if hb.Hostname != "" {
		updates["hostname"] = hb.Hostname
	}
	if err := database.DB.Model(&model.Agent{}).Where("id = ?", agentID).Updates(updates).Error; err != nil {
		log.Printf("更新 Agent(%d) 心跳失败: %v", agentID, err)
	}
}

// State 查询某个托管对象的最新运行态
func (h *Hub) State(agentID uint, targetType string, targetID uint) (proto.TargetState, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st, ok := h.states[agentID][stateKey(targetType, targetID)]
	return st, ok
}

// MarkOffline 标记 Agent 离线并清理缓存
func (h *Hub) MarkOffline(agentID uint) {
	h.mu.Lock()
	delete(h.conns, agentID)
	delete(h.states, agentID)
	h.mu.Unlock()

	now := time.Now()
	if err := database.DB.Model(&model.Agent{}).Where("id = ?", agentID).
		Updates(map[string]any{"status": "offline", "last_seen": now}).Error; err != nil {
		log.Printf("标记 Agent(%d) 离线失败: %v", agentID, err)
	}
}

func stateKey(targetType string, targetID uint) string {
	return fmt.Sprintf("%s:%d", targetType, targetID)
}

func (c *Conn) readLoop() {
	defer c.close()
	for {
		var env proto.Envelope
		if err := c.ws.ReadJSON(&env); err != nil {
			return
		}
		switch env.Type {
		case proto.MsgHeartbeat:
			var hb proto.HeartbeatData
			if err := json.Unmarshal(env.Data, &hb); err == nil {
				c.hub.UpdateState(c.agentID, hb, c.ws.RemoteAddr().String())
			}
		default:
			c.hub.deliver(env)
		}
	}
}

func (c *Conn) writeLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	defer c.close()

	for {
		select {
		case env, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteJSON(env); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteJSON(proto.Envelope{Type: proto.MsgPing}); err != nil {
				return
			}
		}
	}
}

func (h *Hub) deliver(env proto.Envelope) {
	if env.ID == "" {
		return
	}
	h.mu.RLock()
	ch, ok := h.pending[env.ID]
	h.mu.RUnlock()
	if !ok {
		return
	}
	select {
	case ch <- env:
	default:
	}
}

func (c *Conn) close() {
	c.once.Do(func() {
		c.hub.MarkOffline(c.agentID)
		_ = c.ws.Close()
		close(c.done)
	})
}
