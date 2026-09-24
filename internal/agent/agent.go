package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"dfpanel/internal/proto"
)

// Version Agent 版本，构建时可通过 -ldflags "-X dfpanel/internal/agent.Version=x.y.z" 注入
var Version = "dev"

const (
	heartbeatInterval = 30 * time.Second
	maxBackoff        = 30 * time.Second
	logTailBytes      = 64 * 1024
)

// Agent 远端守护程序：一个进程同时可托管多个 frps 与 frpc
type Agent struct {
	cfg    *Config
	client *Client

	mu      sync.Mutex
	ctrls   map[string]*Controller
	targets map[string]Target
	curV    map[string]int
	curS    map[string]string
}

// New 创建 Agent
func New(cfg *Config) *Agent {
	return &Agent{
		cfg:     cfg,
		client:  NewClient(cfg),
		ctrls:   make(map[string]*Controller),
		targets: make(map[string]Target),
		curV:    make(map[string]int),
		curS:    make(map[string]string),
	}
}

// Run 主循环：优先 WebSocket 反连，不可用时降级为 HTTP 轮询
func (a *Agent) Run() error {
	hostname, _ := os.Hostname()
	if err := a.client.Register(Version, hostname); err != nil {
		log.Printf("注册到面板失败（将重试）：%v", err)
	} else {
		log.Printf("已注册到面板 %s（角色：%s，运行时：%s）", a.cfg.PanelURL, a.cfg.Roles, a.cfg.Runtime)
	}

	configsSynced := false
	// 配置文件丢失时，先从面板补回最近成功应用的快照，再按启动状态恢复实例。
	if err := a.syncMissingConfigs(); err != nil {
		log.Printf("从面板恢复缺失配置失败（将重试）：%v", err)
	} else {
		configsSynced = true
	}
	a.bootstrap()

	backoff := time.Second
	for {
		if !configsSynced {
			if err := a.syncMissingConfigs(); err != nil {
				log.Printf("从面板恢复缺失配置失败（将重试）：%v", err)
			} else {
				configsSynced = true
			}
			// 即使个别配置恢复失败，也让本轮已恢复的实例按启动状态运行。
			a.bootstrap()
		}
		conn, err := a.client.DialWS()
		if err == nil {
			log.Printf("长连接已建立：%s", a.cfg.PanelURL)
			backoff = time.Second
			// 配置接口暂时失败但长连接可用时，定时重连以重试恢复，避免一直等到 WS 自然断开。
			var retryTimer *time.Timer
			if !configsSynced {
				retryTimer = time.AfterFunc(30*time.Second, func() { _ = conn.Close() })
			}
			if err := a.serveWS(conn); err != nil {
				log.Printf("长连接中断：%v", err)
			}
			if retryTimer != nil {
				retryTimer.Stop()
			}
		} else {
			log.Printf("连接面板失败（%v），转为轮询模式", err)
		}

		// 轮询兼顾长连接中断期间积压的指令
		a.pollOnce()

		time.Sleep(backoff)
		if backoff < maxBackoff {
			backoff *= 2
		}
	}
}

// serveWS 处理面板下发的实时指令，并定时上报心跳
func (a *Agent) serveWS(conn *websocket.Conn) error {
	defer conn.Close()

	stop := make(chan struct{})
	defer close(stop)

	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := conn.WriteJSON(a.heartbeatEnvelope()); err != nil {
					return
				}
			case <-stop:
				return
			}
		}
	}()

	for {
		var env proto.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			return err
		}
		switch env.Type {
		case proto.MsgCommand:
			var cmd proto.CommandData
			if err := json.Unmarshal(env.Data, &cmd); err != nil {
				log.Printf("解析指令失败：%v", err)
				continue
			}
			res := a.handleCommand(cmd)
			data, _ := json.Marshal(res)
			_ = conn.WriteJSON(proto.Envelope{Type: proto.MsgCommandResult, ID: env.ID, Data: data})
		case proto.MsgPing:
			_ = conn.WriteJSON(proto.Envelope{Type: proto.MsgPong})
		}
	}
}

// pollOnce 拉取并执行积压指令，同时上报心跳
func (a *Agent) pollOnce() {
	cmds, err := a.client.PullCommands()
	if err != nil {
		return
	}
	for _, cmd := range cmds {
		res := a.handleCommand(cmd)
		if err := a.client.Report(cmd.CommandID, res); err != nil {
			log.Printf("上报指令(%d)结果失败：%v", cmd.CommandID, err)
		}
	}
	_ = a.client.Heartbeat(a.heartbeatPayload())
}

// handleCommand 执行单条指令
func (a *Agent) handleCommand(cmd proto.CommandData) proto.ResultData {
	t := Target{Type: cmd.TargetType, ID: cmd.TargetID}

	switch cmd.Type {
	case proto.CmdApply:
		a.applyAutoStartFlag(t, cmd.Flags)
		out := a.ApplyConfig(t, cmd.Payload, cmd.Version)
		return proto.ResultData{
			OK:         out.OK,
			Message:    out.Message,
			Version:    out.Version,
			Checksum:   out.Checksum,
			RolledBack: out.RolledBack,
			Unverified: out.Unverified,
			Running:    a.controller(t).Running(),
		}
	case proto.CmdStart:
		// 二进制缺失时先补齐（新补槽位要重建控制器，才能带上挂载信息）
		if created, err := a.ensureRuntimeBinary(kindOf(t)); err != nil {
			return proto.ResultData{Message: "启动失败：" + err.Error()}
		} else if created {
			a.resetControllers()
		}
		ctrl := a.controller(t)
		if err := ctrl.Start(); err != nil {
			return proto.ResultData{Message: "启动失败：" + err.Error()}
		}
		// 手动启动过：重新纳入自动启动
		a.updateState(targetKey(t), func(s *instanceState) { s.ManualStopped = false })
		if reason := a.confirmRunning(t); reason != "" {
			return proto.ResultData{Message: reason, Running: false}
		}
		return proto.ResultData{OK: true, Message: "已启动", Running: true}
	case proto.CmdStop:
		ctrl := a.controller(t)
		if err := ctrl.Stop(); err != nil {
			return proto.ResultData{Message: "停止失败：" + err.Error()}
		}
		// 手动停过：Agent 重启后不再自动拉起
		a.updateState(targetKey(t), func(s *instanceState) { s.ManualStopped = true })
		return proto.ResultData{OK: true, Message: "已停止"}
	case proto.CmdRestart:
		if created, err := a.ensureRuntimeBinary(kindOf(t)); err != nil {
			return proto.ResultData{Message: "重启失败：" + err.Error()}
		} else if created {
			a.resetControllers()
		}
		ctrl := a.controller(t)
		if err := ctrl.Restart(); err != nil {
			return proto.ResultData{Message: "重启失败：" + err.Error()}
		}
		// 手动重启过：重新纳入自动启动
		a.updateState(targetKey(t), func(s *instanceState) { s.ManualStopped = false })
		if reason := a.confirmRunning(t); reason != "" {
			return proto.ResultData{Message: reason, Running: false}
		}
		return proto.ResultData{OK: true, Message: "已重启", Running: true}
	case proto.CmdLog:
		ctrl := a.controller(t)
		logs, err := ctrl.Logs(logTailBytes)
		if err != nil {
			return proto.ResultData{Message: "读取日志失败：" + err.Error()}
		}
		return proto.ResultData{OK: true, Content: logs, Running: ctrl.Running()}
	case proto.CmdRollback:
		return a.handleRollback(t, cmd)

	case proto.CmdVersions:
		return proto.ResultData{OK: true, Versions: a.listHistory(t), Running: a.controller(t).Running()}

	case proto.CmdVersionConfig:
		if cmd.Version <= 0 {
			return proto.ResultData{Message: "版本号不合法"}
		}
		content, err := a.readHistory(t, cmd.Version)
		if err != nil {
			return proto.ResultData{Message: err.Error()}
		}
		return proto.ResultData{OK: true, Content: content}

	case proto.CmdFrpActivate:
		return a.handleFrpActivate(cmd)

	case proto.CmdFrpStatus:
		active, cached := a.frpStatus()
		return proto.ResultData{
			OK:         true,
			Message:    a.frpPlatformError(),
			FrpVersion: active,
			FrpCached:  cached,
		}
	}
	return proto.ResultData{Message: "未知指令：" + cmd.Type}
}

// handleRollback 回滚到面板指定的历史版本；未指定版本时退回上一版备份（.bak）。
// 回滚同样走完整的校验与健康探测流程，失败会再次回退到回滚前的配置。
func (a *Agent) handleRollback(t Target, cmd proto.CommandData) proto.ResultData {
	var req struct {
		TargetVersion int `json:"targetVersion"`
	}
	if cmd.Payload != "" {
		_ = json.Unmarshal([]byte(cmd.Payload), &req)
	}

	if req.TargetVersion <= 0 {
		out := a.rollback(t, a.controller(t), "面板触发回滚", true)
		return proto.ResultData{
			OK:         out.OK,
			RolledBack: true,
			Message:    out.Message,
			Running:    a.controller(t).Running(),
		}
	}

	content, err := a.readHistory(t, req.TargetVersion)
	if err != nil {
		return proto.ResultData{Message: err.Error()}
	}

	out := a.ApplyConfig(t, content, cmd.Version)
	return proto.ResultData{
		OK:         out.OK,
		Message:    fmt.Sprintf("已回滚到历史版本 v%d：%s", req.TargetVersion, out.Message),
		Version:    out.Version,
		Checksum:   out.Checksum,
		Unverified: out.Unverified,
		Running:    a.controller(t).Running(),
	}
}

// bootstrap 扫描数据目录中已有的 frp 配置，拉起未运行的实例（Agent 重启后自愈）
func (a *Agent) bootstrap() {
	entries, err := os.ReadDir(a.cfg.DataDir)
	if err != nil {
		return
	}
	// 二进制由面板下发，先把启用角色的槽位补齐，再决定拉起哪些实例
	for _, kind := range a.enabledKinds() {
		created, err := a.ensureRuntimeBinary(kind)
		if err != nil {
			log.Printf("准备 %s 二进制失败：%v", kind, err)
			continue
		}
		if created {
			log.Printf("已从面板获取 %s 二进制", kind)
			a.resetControllers()
		}
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := e.Name()

		var kind, typ string
		switch {
		case strings.HasPrefix(name, "frps-"):
			kind, typ = "frps", proto.TargetServer
		case strings.HasPrefix(name, "frpc-"):
			kind, typ = "frpc", proto.TargetNode
		default:
			continue
		}
		id, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(name, kind+"-"), ".json"), 10, 64)
		if err != nil {
			continue
		}
		if !a.cfg.HasRole(kind) {
			continue
		}

		t := Target{Type: typ, ID: uint(id)}
		ctrl := a.controller(t)
		if ctrl.Running() {
			continue
		}
		// 上个 Agent 进程留下的实例还在跑：接管它，别再起一个（会撞端口）。
		// 接管只是认领已在运行的进程，与「是否自动启动」无关 ——
		// 没开自动启动也不该放任它跑着却不受管。
		if ctrl.Adopt() {
			log.Printf("已接管仍在运行的实例 %s-%d", kind, id)
			continue
		}
		// 有记录就按记录来：没开自动启动、或被手动停过的不拉起来；
		// 没记录的是旧版本留下的实例，维持原来的自愈行为。
		if st, tracked := a.trackedState(targetKey(t)); tracked && (!st.AutoStart || st.ManualStopped) {
			log.Printf("跳过 %s-%d（未开启自动启动或已被手动停止）", kind, id)
			continue
		}
		if err := ctrl.Start(); err != nil {
			log.Printf("恢复 %s-%d 失败：%v", kind, id, err)
			continue
		}
		log.Printf("已恢复本地实例 %s-%d", kind, id)
	}
}

// controller 获取（或创建）目标对应的控制器
func (a *Agent) controller(t Target) *Controller {
	key := targetKey(t)

	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.ctrls[key]; ok {
		return c
	}

	kind := kindOf(t)
	spec := Spec{
		Kind:          kind,
		Runtime:       a.cfg.Runtime,
		BinPath:       a.cfg.BinaryPath(kind),
		ConfigPath:    a.cfg.ConfigPath(t),
		LogPath:       a.cfg.LogPath(t),
		PidPath:       a.cfg.PidPath(t),
		ContainerName: ContainerName(kind, t.ID),
	}
	if a.cfg.Runtime == "docker" {
		// 容器底座里没有 frp：必须拿到面板下发的二进制才起得来（启动时用 docker cp 拷进容器）。
		// 槽位没就绪（面板还没下发过、或 docker 不可用）时记下原因，启动那一步会直接拒绝。
		cos, carch, err := containerPlatform()
		switch {
		case err != nil:
			spec.MountErr = err.Error()
		default:
			slot := a.cfg.ContainerSlotPath(kind, cos, carch)
			if fileReady(slot) {
				spec.MountBinary = slot
			} else {
				spec.MountErr = fmt.Sprintf("容器槽位 %s 还没有二进制，%s 尚未由面板下发", slot, kind)
			}
		}
	}
	if kind == "frps" {
		spec.Image = a.cfg.FrpsImage
	} else {
		spec.Image = a.cfg.FrpcImage
	}

	c := NewController(spec)
	a.ctrls[key] = c
	a.targets[key] = t
	return c
}

func (a *Agent) setCurrent(t Target, version int, checksum string) {
	key := targetKey(t)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.curV[key] = version
	a.curS[key] = checksum
}

func (a *Agent) heartbeatPayload() proto.HeartbeatData {
	hostname, _ := os.Hostname()

	a.mu.Lock()
	targets := make([]proto.TargetState, 0, len(a.ctrls))
	for key, c := range a.ctrls {
		t, ok := a.targets[key]
		if !ok {
			continue
		}
		targets = append(targets, proto.TargetState{
			TargetType: t.Type,
			TargetID:   t.ID,
			Running:    c.Running(),
			Version:    a.curV[key],
			Checksum:   a.curS[key],
		})
	}
	a.mu.Unlock()

	active, cached := a.frpStatus()
	return proto.HeartbeatData{
		Version:    Version,
		Hostname:   hostname,
		Targets:    targets,
		Runtime:    a.cfg.Runtime,
		FrpVersion: active,
		FrpCached:  cached,
	}
}

func (a *Agent) heartbeatEnvelope() proto.Envelope {
	data, _ := json.Marshal(a.heartbeatPayload())
	return proto.Envelope{Type: proto.MsgHeartbeat, Data: data}
}

func targetKey(t Target) string {
	return fmt.Sprintf("%s:%d", t.Type, t.ID)
}

// DataDir 暴露数据目录，便于命令行查看
func (a *Agent) DataDir() string {
	return filepath.Clean(a.cfg.DataDir)
}
