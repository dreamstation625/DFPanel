package web

import "embed"

// Dist 前端构建产物（由 web 目录执行 npm run build 生成）
//
//go:embed all:dist
var Dist embed.FS
