// Package setting 管理面板运行期可改的系统设置。
//
// 取值优先级：数据库设置（非空） > 启动参数/环境变量 > 内置默认。
// 之所以让数据库优先，是为了让运维在设置页改了下载镜像源之后无需重启面板。
package setting

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"dfpanel/internal/database"
	"dfpanel/internal/distrib"
	"dfpanel/internal/model"
)

// 设置项 key
const (
	KeyFrpDownloadBase   = "frpDownloadBase"   // 下载地址模板（支持镜像源）
	KeyFrpManualVersions = "frpManualVersions" // 手填版本列表，逗号或换行分隔
	KeyPanelFrpVersion   = "panelFrpVersion"   // 面板本机 frps 使用的版本
)

// Values 设置项集合
type Values struct {
	// FrpDownloadBase 下载地址模板
	FrpDownloadBase string `json:"frpDownloadBase"`
	// FrpManualVersions 手填兜底版本，逗号分隔
	FrpManualVersions string `json:"frpManualVersions"`
	// PanelFrpVersion 面板本机 frps 版本，空表示不管理
	PanelFrpVersion string `json:"panelFrpVersion"`
	// FrpVersionAPI 版本列表接口，固定为 GitHub 官方，只读回显
	FrpVersionAPI string `json:"frpVersionApi"`
	// FrpDownloadBaseDefault 默认模板，便于前端给出「恢复默认」提示
	FrpDownloadBaseDefault string `json:"frpDownloadBaseDefault"`
}

// Load 读取设置，fallbackDownloadBase 来自启动参数/环境变量
func Load(fallbackDownloadBase string) Values {
	base := Get(KeyFrpDownloadBase)
	if base == "" {
		base = strings.TrimSpace(fallbackDownloadBase)
	}
	if base == "" {
		base = distrib.DefaultDownloadBase
	}
	return Values{
		FrpDownloadBase:        base,
		FrpManualVersions:      Get(KeyFrpManualVersions),
		PanelFrpVersion:        Get(KeyPanelFrpVersion),
		FrpVersionAPI:          distrib.VersionListAPI,
		FrpDownloadBaseDefault: distrib.DefaultDownloadBase,
	}
}

// Save 校验并保存设置
func Save(v Values) error {
	base := strings.TrimSpace(v.FrpDownloadBase)
	if base == "" {
		base = distrib.DefaultDownloadBase
	}
	if err := ValidateDownloadBase(base); err != nil {
		return err
	}
	raw, err := NormalizeManualVersions(v.FrpManualVersions)
	if err != nil {
		return err
	}
	panelVer := strings.TrimSpace(v.PanelFrpVersion)
	if panelVer != "" && !distrib.ValidVersion(panelVer) {
		return fmt.Errorf("面板本机 frp 版本号不合法：%s", panelVer)
	}

	for k, val := range map[string]string{
		KeyFrpDownloadBase:   base,
		KeyFrpManualVersions: raw,
		KeyPanelFrpVersion:   panelVer,
	} {
		if err := put(k, val); err != nil {
			return err
		}
	}
	return nil
}

// ValidateDownloadBase 校验下载地址模板
func ValidateDownloadBase(base string) error {
	if !strings.Contains(base, "{version}") || !strings.Contains(base, "{asset}") {
		return errors.New("下载地址模板必须同时包含 {version} 与 {asset} 占位符")
	}
	// 用占位符之外的形态做一次 URL 合法性检查
	probe := strings.NewReplacer("{version}", "0.0.0", "{asset}", "frp_0.0.0_linux_amd64.tar.gz",
		"{os}", "linux", "{arch}", "amd64").Replace(base)
	u, err := url.Parse(probe)
	if err != nil {
		return fmt.Errorf("下载地址模板不是合法 URL：%w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("下载地址模板必须以 http:// 或 https:// 开头")
	}
	if u.Host == "" {
		return errors.New("下载地址模板缺少主机名")
	}
	return nil
}

// NormalizeManualVersions 归一化手填版本（逗号/空白/换行分隔），并剔除非法项
func NormalizeManualVersions(raw string) (string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t' || r == ';'
	})
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	invalid := make([]string, 0)
	for _, f := range fields {
		v := strings.TrimPrefix(strings.TrimSpace(f), "v")
		if v == "" {
			continue
		}
		if !distrib.ValidVersion(v) {
			invalid = append(invalid, f)
			continue
		}
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	if len(invalid) > 0 {
		return "", fmt.Errorf("以下版本号格式不合法：%s", strings.Join(invalid, ", "))
	}
	return strings.Join(out, ","), nil
}

// ManualVersions 手填版本切片
func ManualVersions() []string {
	raw := Get(KeyFrpManualVersions)
	if raw == "" {
		return nil
	}
	return strings.Split(raw, ",")
}

// Get 读取单项设置，不存在返回空串
func Get(key string) string {
	var s model.Setting
	if err := database.DB.First(&s, "key = ?", key).Error; err != nil {
		return ""
	}
	return s.Value
}

func put(key, value string) error {
	var count int64
	database.DB.Model(&model.Setting{}).Where("key = ?", key).Count(&count)
	if count == 0 {
		return database.DB.Create(&model.Setting{Key: key, Value: value}).Error
	}
	return database.DB.Model(&model.Setting{}).Where("key = ?", key).
		Update("value", value).Error
}
