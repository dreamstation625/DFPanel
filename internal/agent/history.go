package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"dfpanel/internal/proto"
)

// historyKeep 每个目标保留的历史配置份数（超出后删除最旧的）
const historyKeep = 30

// historyDir 历史配置目录：<dataDir>/history
func (a *Agent) historyDir() string {
	return filepath.Join(a.cfg.DataDir, "history")
}

// historyFileName 形如 server-1.v3.json / node-7.v12.json
func historyFileName(t Target, version int) string {
	return fmt.Sprintf("%s-%d.v%d.json", kindOf(t), t.ID, version)
}

func (a *Agent) historyPath(t Target, version int) string {
	return filepath.Join(a.historyDir(), historyFileName(t, version))
}

// saveHistory 每次收到下发的配置（无论最终成败）都落一份快照，供面板查看与回滚
func (a *Agent) saveHistory(t Target, version int, content string) {
	if version <= 0 || content == "" {
		return
	}
	if err := os.MkdirAll(a.historyDir(), 0o755); err != nil {
		return
	}
	_ = writeFileAtomic(a.historyPath(t, version), content)
	a.pruneHistory(t)
}

// pruneHistory 只保留最近 historyKeep 份，避免磁盘无限增长
func (a *Agent) pruneHistory(t Target) {
	entries := a.listHistory(t)
	if len(entries) <= historyKeep {
		return
	}
	// entries 已按版本倒序，删除尾部
	for _, e := range entries[historyKeep:] {
		_ = os.Remove(a.historyPath(t, e.Version))
	}
}

// historyPrefix 该目标的历史文件前缀
func historyPrefix(t Target) string {
	return fmt.Sprintf("%s-%d.v", kindOf(t), t.ID)
}

// listHistory 列出历史版本（按版本号倒序），并标记哪一份与当前生效配置一致
func (a *Agent) listHistory(t Target) []proto.HistoryEntry {
	dir, err := os.ReadDir(a.historyDir())
	if err != nil {
		return nil
	}

	prefix := historyPrefix(t)
	curSum := a.currentChecksum(t)

	out := make([]proto.HistoryEntry, 0, len(dir))
	for _, de := range dir {
		name := de.Name()
		if de.IsDir() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		verStr := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json")
		version, err := strconv.Atoi(verStr)
		if err != nil {
			continue
		}
		full := filepath.Join(a.historyDir(), name)
		fi, err := os.Stat(full)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		sum := sha256.Sum256(data)
		checksum := hex.EncodeToString(sum[:])

		out = append(out, proto.HistoryEntry{
			Version:  version,
			Size:     fi.Size(),
			Time:     fi.ModTime().Unix(),
			Checksum: checksum,
			Current:  curSum != "" && curSum == checksum,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out
}

// readHistory 读取指定历史版本的配置内容
func (a *Agent) readHistory(t Target, version int) (string, error) {
	data, err := os.ReadFile(a.historyPath(t, version))
	if err != nil {
		return "", fmt.Errorf("历史版本 v%d 不存在或不可读", version)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("历史版本 v%d 内容为空", version)
	}
	return string(data), nil
}

// currentChecksum 当前生效配置的指纹（用文件内容比对，Agent 重启后依然准确）
func (a *Agent) currentChecksum(t Target) string {
	data, err := os.ReadFile(a.cfg.ConfigPath(t))
	if err != nil || len(data) == 0 {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
