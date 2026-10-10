// 测试用进程，仅模拟 verify、监听成功及启动后退出，不下载或执行第三方二进制。
package main

import (
	"encoding/json"
	"net"
	"os"
	"strconv"
)

func main() {
	args := os.Args[1:]
	verify := len(args) > 0 && args[0] == "verify"
	if verify {
		args = args[1:]
	}
	if len(args) != 2 || args[0] != "-c" {
		os.Exit(2)
	}
	b, err := os.ReadFile(args[1])
	if err != nil {
		os.Exit(2)
	}
	var cfg struct {
		BindPort   int  `json:"bindPort"`
		FailVerify bool `json:"failVerify"`
		FailStart  bool `json:"failStart"`
	}
	if json.Unmarshal(b, &cfg) != nil || cfg.FailVerify {
		os.Exit(3)
	}
	if verify {
		return
	}
	if cfg.FailStart {
		os.Exit(4)
	}
	l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(cfg.BindPort)))
	if err != nil {
		os.Exit(5)
	}
	defer l.Close()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		c.Close()
	}
}
