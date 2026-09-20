// Package distrib 内嵌分发给客户端的安装脚本，面板无需外链即可提供一键安装
package distrib

import _ "embed"

// InstallSh Linux / macOS 一键安装脚本
//
//go:embed install.sh
var InstallSh string

// InstallPs1 Windows 一键安装脚本
//
//go:embed install.ps1
var InstallPs1 string
