package proto

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RequestSign v2 将身份、请求方法、完整路径、正文摘要与一次性随机数一起签名。
func RequestSign(secret string, r *http.Request, body []byte) string {
	sum := sha256.Sum256(body)
	fields := []string{"v2", r.Method, r.URL.RequestURI(), r.Header.Get("X-Node-Key"), r.Header.Get("X-Agent-Instance"), r.Header.Get("X-Agent-Host"), r.Header.Get("X-Agent-Role"), r.Header.Get("X-Node-Ts"), r.Header.Get("X-Node-Nonce"), hex.EncodeToString(sum[:])}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join(fields, "\n")))
	return hex.EncodeToString(mac.Sum(nil))
}

func AuthorizeRequest(r *http.Request, secret, nodeKey, instance, host, role string, body []byte) error {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	r.Header.Set("X-Node-Auth", "v2")
	r.Header.Set("X-Node-Key", nodeKey)
	r.Header.Set("X-Agent-Instance", instance)
	r.Header.Set("X-Agent-Host", host)
	r.Header.Set("X-Agent-Role", role)
	r.Header.Set("X-Node-Ts", strconv.FormatInt(time.Now().Unix(), 10))
	r.Header.Set("X-Node-Nonce", hex.EncodeToString(b))
	r.Header.Set("X-Node-Sign", RequestSign(secret, r, body))
	return nil
}

func VerifyRequest(r *http.Request, secret string, body []byte) error {
	if r.Header.Get("X-Node-Auth") != "v2" {
		return fmt.Errorf("需要 v2 签名，请同步更新 Agent")
	}
	ts, err := strconv.ParseInt(r.Header.Get("X-Node-Ts"), 10, 64)
	if err != nil {
		return fmt.Errorf("时间戳不合法")
	}
	delta := time.Since(time.Unix(ts, 0))
	if delta > DefaultSkew || delta < -DefaultSkew {
		return fmt.Errorf("时间戳超出允许范围")
	}
	nonce, err := hex.DecodeString(r.Header.Get("X-Node-Nonce"))
	if err != nil || len(nonce) != 32 {
		return fmt.Errorf("随机数不合法")
	}
	if !hmac.Equal([]byte(RequestSign(secret, r, body)), []byte(r.Header.Get("X-Node-Sign"))) {
		return fmt.Errorf("签名校验失败")
	}
	return nil
}
