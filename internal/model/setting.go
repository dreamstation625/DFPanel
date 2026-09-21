package model

import "time"

// Setting 面板运行期可改的系统设置（key-value）。
// 用单表而非加列，便于后续继续追加配置项而不动结构。
type Setting struct {
	Key       string    `gorm:"primaryKey;size:64" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}
