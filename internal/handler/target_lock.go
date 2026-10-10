package handler

import (
	"fmt"
	"sync"
)

type targetLock struct {
	mu    sync.Mutex
	users int
}

var targetLocksMu sync.Mutex
var targetLocks = map[string]*targetLock{}

// lockTarget 使同一实例的快照生成、入队和本机操作保持同一顺序，空闲后释放锁条目。
func lockTarget(kind string, id uint) func() {
	key := fmt.Sprintf("%s/%d", kind, id)
	targetLocksMu.Lock()
	l := targetLocks[key]
	if l == nil {
		l = &targetLock{}
		targetLocks[key] = l
	}
	l.users++
	targetLocksMu.Unlock()
	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		targetLocksMu.Lock()
		l.users--
		if l.users == 0 {
			delete(targetLocks, key)
		}
		targetLocksMu.Unlock()
	}
}
