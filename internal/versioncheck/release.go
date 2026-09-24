package versioncheck

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	releaseAPI = "https://api.github.com/repos/dreamstation625/DFPanel/releases?per_page=100"
	rawBase    = "https://raw.githubusercontent.com/dreamstation625/DFPanel"
	releaseURL = "https://github.com/dreamstation625/DFPanel/releases/tag/"
)

// Published 是一个检测通道的最新版本。Agent 版本从发布 tag 的 VERSION.agent 读取。
type Published struct {
	PanelVersion    string
	AgentVersion    string
	ReleaseURL      string
	AgentReleaseURL string
	AgentError      string
}

// Latest 分别保留全部发布和正式发布，运行正式版时只使用 Stable。
type Latest struct {
	Any       Published
	Stable    Published
	CheckedAt time.Time
}

type Client struct {
	httpClient *http.Client
	apiURL     string
	rawURL     string
	mu         sync.Mutex
	cached     Latest
	cachedErr  error
	cachedAt   time.Time
}

func NewClient() *Client {
	return &Client{httpClient: &http.Client{Timeout: 15 * time.Second}, apiURL: releaseAPI, rawURL: rawBase}
}

var Default = NewClient()

// Check 查询包含预发布版的 Release 列表，并缓存 10 分钟；手动刷新最短间隔 30 秒。
func (client *Client) Check(ctx context.Context, refresh bool) (Latest, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if !client.cachedAt.IsZero() &&
		(time.Since(client.cachedAt) < 30*time.Second || (!refresh && time.Since(client.cachedAt) < 10*time.Minute)) {
		return client.cached, client.cachedErr
	}
	result, err := client.fetch(ctx)
	client.cached = result
	client.cachedErr = err
	client.cachedAt = time.Now()
	return result, err
}

func (client *Client) fetch(ctx context.Context) (Latest, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, client.apiURL, nil)
	if err != nil {
		return Latest{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "DFPanel-version-check")
	resp, err := client.httpClient.Do(req)
	if err != nil {
		return Latest{}, fmt.Errorf("查询 GitHub Releases 失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Latest{}, fmt.Errorf("查询 GitHub Releases 失败：HTTP %d", resp.StatusCode)
	}
	var releases []struct {
		TagName string `json:"tag_name"`
		Draft   bool   `json:"draft"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&releases); err != nil {
		return Latest{}, fmt.Errorf("解析 GitHub Releases 失败：%w", err)
	}
	type releaseTag struct{ tag, version string }
	var validReleases []releaseTag
	var stableReleases []releaseTag
	for _, release := range releases {
		version := strings.TrimPrefix(release.TagName, "v")
		if release.Draft {
			continue
		}
		isPre, valid := IsPrerelease(version)
		if !valid {
			continue
		}
		item := releaseTag{tag: release.TagName, version: version}
		validReleases = append(validReleases, item)
		if !isPre {
			stableReleases = append(stableReleases, item)
		}
	}
	if len(validReleases) == 0 {
		return Latest{}, fmt.Errorf("GitHub Releases 中没有可识别的版本")
	}
	sort.Slice(validReleases, func(i, j int) bool {
		order, _ := Compare(validReleases[i].version, validReleases[j].version)
		return order > 0
	})
	sort.Slice(stableReleases, func(i, j int) bool {
		order, _ := Compare(stableReleases[i].version, stableReleases[j].version)
		return order > 0
	})
	result := Latest{CheckedAt: time.Now()}
	newest := validReleases[0]
	result.Any = Published{PanelVersion: newest.version, ReleaseURL: releaseURL + newest.tag}
	result.Any.AgentVersion, result.Any.AgentError = client.agentVersion(ctx, newest.tag, newest.version)
	result.Any.AgentReleaseURL = result.Any.ReleaseURL
	if len(stableReleases) == 0 {
		return result, nil
	}
	stable := stableReleases[0]
	result.Stable = Published{PanelVersion: stable.version, ReleaseURL: releaseURL + stable.tag}
	for _, candidate := range stableReleases {
		agentVersion, agentError := "", ""
		if candidate.tag == newest.tag {
			agentVersion, agentError = result.Any.AgentVersion, result.Any.AgentError
		} else {
			agentVersion, agentError = client.agentVersion(ctx, candidate.tag, candidate.version)
		}
		if agentError != "" {
			result.Stable.AgentError = agentError
			break
		}
		isPre, valid := IsPrerelease(agentVersion)
		if valid && !isPre {
			result.Stable.AgentVersion = agentVersion
			result.Stable.AgentReleaseURL = releaseURL + candidate.tag
			break
		}
	}
	return result, nil
}

// agentVersion 读取指定发布 tag 的独立 Agent 版本；文件不存在时按构建脚本回退到面板版本。
func (client *Client) agentVersion(ctx context.Context, tag, fallback string) (string, string) {
	agentURL := strings.TrimRight(client.rawURL, "/") + "/" + tag + "/VERSION.agent"
	agentReq, err := http.NewRequestWithContext(ctx, http.MethodGet, agentURL, nil)
	if err != nil {
		return "", err.Error()
	}
	agentReq.Header.Set("User-Agent", "DFPanel-version-check")
	agentResp, err := client.httpClient.Do(agentReq)
	if err != nil {
		return "", fmt.Sprintf("查询 Agent 发布版本失败：%v", err)
	}
	defer agentResp.Body.Close()
	if agentResp.StatusCode == http.StatusNotFound {
		return fallback, ""
	}
	if agentResp.StatusCode != http.StatusOK {
		return "", fmt.Sprintf("查询 Agent 发布版本失败：HTTP %d", agentResp.StatusCode)
	}
	content, err := io.ReadAll(io.LimitReader(agentResp.Body, 256))
	if err != nil {
		return "", fmt.Sprintf("读取 Agent 发布版本失败：%v", err)
	}
	agentVersion := strings.TrimSpace(string(content))
	if _, valid := Compare(agentVersion, agentVersion); !valid {
		return "", "发布文件 VERSION.agent 中的版本号无效"
	}
	return agentVersion, ""
}
