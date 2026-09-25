package main

import (
	"context"
	"flag"
	"log"
	"os"
	"path/filepath"
	"strings"

	"dfpanel/internal/agentbundle"
)

func main() {
	output := flag.String("output", "", "Agent 全平台包输出路径")
	flag.Parse()
	if *output == "" {
		log.Fatal("需要 -output")
	}
	root, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	panelVersion, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		log.Fatal(err)
	}
	agentVersion, err := os.ReadFile(filepath.Join(root, "VERSION.agent"))
	if err != nil {
		log.Fatal(err)
	}
	if err := agentbundle.Create(context.Background(), root, *output, strings.TrimSpace(string(panelVersion)), strings.TrimSpace(string(agentVersion))); err != nil {
		log.Fatal(err)
	}
	log.Printf("已生成 Agent 全平台包：%s", *output)
}
