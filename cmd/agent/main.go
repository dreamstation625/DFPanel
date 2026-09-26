// Command agent 远端守护程序：与 DFPanel 面板通信，托管 frps / frpc 的启停、配置应用与失败回滚。
//
// 配置来源优先级：命令行 --config > 环境变量 > agent.json
// 常用环境变量：DFPANEL_URL / DFPANEL_NODE_KEY / DFPANEL_NODE_SECRET / DFPANEL_ROLES / DFPANEL_RUNTIME / DFPANEL_DATA_DIR
package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"dfpanel/internal/agent"
)

func main() {
	configPath := flag.String("config", "", "agent.json 路径（留空则使用环境变量）")
	showVersion := flag.Bool("version", false, "打印版本")
	flag.Parse()

	if *showVersion {
		log.Printf("dfpanel-agent %s", agent.Version)
		return
	}

	cfg, err := agent.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("加载配置失败：%v", err)
	}
	logFile, err := openAgentLog(cfg.DataDir)
	if err != nil {
		log.Fatal(err)
	}
	defer logFile.Close()
	// 先写文件；计划任务没有可用标准错误句柄时，也不能丢失本地日志。
	log.SetOutput(io.MultiWriter(logFile, os.Stderr))
	log.Printf("dfpanel-agent %s 启动；日志：%s", agent.Version, logFile.path)

	// 收到终止信号时直接退出：容器形态交给 restart policy，系统服务交给 systemd / 计划任务
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("收到退出信号，Agent 停止（已运行的 frp 实例保持后台运行）")
		os.Exit(0)
	}()

	a := agent.New(cfg)
	if err := a.Run(); err != nil {
		log.Fatalf("Agent 退出：%v", err)
	}
}
