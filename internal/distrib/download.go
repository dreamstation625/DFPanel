package distrib

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	frpReleaseAPI = "https://api.github.com/repos/fatedier/frp/releases/latest"
	frpDownload   = "https://github.com/fatedier/frp/releases/download"
)

// BinaryName 本地缓存文件名：frps-linux-amd64 / frpc.exe 形态
func BinaryName(kind, goos, goarch string) string {
	name := fmt.Sprintf("%s-%s-%s", kind, goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// LatestVersion 查询 frp 最新版本号（不带 v 前缀）
func LatestVersion() (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(frpReleaseAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("查询最新版本失败：HTTP %d", resp.StatusCode)
	}
	var out struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return strings.TrimPrefix(strings.TrimSpace(out.TagName), "v"), nil
}

// EnsureFRPBinary 确保本地存在 frp 二进制；缺失时从官方 release 下载并缓存到 dir。
// version 传 "latest" 时自动解析最新版本号。
func EnsureFRPBinary(kind, version, goos, goarch, dir string) (string, error) {
	if kind != "frps" && kind != "frpc" {
		return "", fmt.Errorf("未知类型：%s", kind)
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	dest := filepath.Join(dir, BinaryName(kind, goos, goarch))
	if fi, err := os.Stat(dest); err == nil && fi.Size() > 0 {
		return dest, nil
	}

	ver := version
	if ver == "" || ver == "latest" {
		v, err := LatestVersion()
		if err != nil {
			return "", fmt.Errorf("解析最新 frp 版本失败：%w", err)
		}
		ver = v
	}

	asset := fmt.Sprintf("frp_%s_%s_%s.tar.gz", ver, normalizeOS(goos), normalizeArch(goarch))
	if goos == "windows" {
		asset = fmt.Sprintf("frp_%s_%s_%s.zip", ver, normalizeOS(goos), normalizeArch(goarch))
	}
	url := fmt.Sprintf("%s/v%s/%s", frpDownload, ver, asset)

	tmp, err := os.CreateTemp("", "frp-download-*")
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
	}()

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载 %s 失败：HTTP %d（%s）", asset, resp.StatusCode, url)
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		return "", err
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	switch {
	case strings.HasSuffix(asset, ".zip"):
		if err := extractZip(tmp.Name(), kind, dest); err != nil {
			return "", err
		}
	default:
		if err := extractTarGz(tmp, kind, dest); err != nil {
			return "", err
		}
	}
	_ = os.Chmod(dest, 0o755)
	return dest, nil
}

func extractTarGz(r io.Reader, kind, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()

	want := kind
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(hdr.Name) != want {
			continue
		}
		f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()
			return err
		}
		return f.Close()
	}
	return errors.New("压缩包中未找到 " + want)
}

func extractZip(path, kind, dest string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()

	want := kind + ".exe"
	for _, f := range zr.File {
		if filepath.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()

		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = out.Close()
			return err
		}
		return out.Close()
	}
	return errors.New("压缩包中未找到 " + want)
}

func normalizeOS(goos string) string {
	switch goos {
	case "darwin":
		return "darwin"
	case "windows":
		return "windows"
	default:
		return "linux"
	}
}

func normalizeArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return "arm"
	}
}
