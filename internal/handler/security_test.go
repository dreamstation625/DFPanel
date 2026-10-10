package handler

import (
	"bytes"
	"dfpanel/internal/agenthub"
	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/middleware"
	"dfpanel/internal/model"
	"dfpanel/internal/proto"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func securityDB(t *testing.T) {
	t.Helper()
	previous := database.DB
	if err := database.Init(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	db, _ := database.DB.DB()
	t.Cleanup(func() { db.Close(); database.DB = previous })
}

func TestInitCreatesOnlyOneAdministrator(t *testing.T) {
	securityDB(t)
	h := NewAuthHandler(&config.Config{})
	r := ginRouter()
	r.POST("/api/init", h.Init)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf(`{"username":"admin%d","password":"correct-password"}`, i)
			r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/init", strings.NewReader(body)))
		}(i)
	}
	wg.Wait()
	var count int64
	if err := database.DB.Model(&model.User{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("管理员数=%d err=%v", count, err)
	}
}

func TestAgentV2RejectsTamperingAndReplay(t *testing.T) {
	securityDB(t)
	a := model.Agent{Name: "owner", NodeKey: "key", Secret: "secret", Roles: "frpc"}
	database.DB.Create(&a)
	h := NewAgentHandler(agenthub.New())
	r := ginRouter()
	r.POST("/api/agent/register", h.Register)
	body := []byte(`{"hostname":"original"}`)
	newReq := func() *http.Request {
		req := httptest.NewRequest("POST", "/api/agent/register", bytes.NewReader(body))
		proto.AuthorizeRequest(req, a.Secret, a.NodeKey, "0123456789abcdef0123456789abcdef", "11111111111111111111111111111111", a.Roles, body)
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	for _, mutate := range []func(*http.Request){func(req *http.Request) { req.Method = "GET" }, func(req *http.Request) { req.URL.Path = "/api/agent/heartbeat" }, func(req *http.Request) { req.Header.Set("X-Agent-Host", "22222222222222222222222222222222") }, func(req *http.Request) { req.Body = http.NoBody }} {
		req := newReq()
		mutate(req)
		if proto.VerifyRequest(req, a.Secret, func() []byte {
			if req.Body == http.NoBody {
				return nil
			}
			return body
		}()) == nil {
			t.Fatal("篡改请求被接受")
		}
	}
	req := newReq()
	headers := req.Header.Clone()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	replay := httptest.NewRequest("POST", "/api/agent/register", bytes.NewReader(body))
	replay.Header = headers
	w = httptest.NewRecorder()
	r.ServeHTTP(w, replay)
	if w.Code != 401 {
		t.Fatalf("重放 status=%d", w.Code)
	}
	var n int64
	database.DB.Model(&model.AgentNonce{}).Count(&n)
	if n != 1 {
		t.Fatalf("持久化随机数=%d", n)
	}
	old := httptest.NewRequest("POST", "/api/agent/register?nodeKey=key&sign="+proto.Sign(a.Secret, a.NodeKey, time.Now().Unix()), bytes.NewReader(body))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, old)
	if w.Code != 401 {
		t.Fatal("旧签名被接受")
	}
}

func TestQueueLeaseOrderAndIdempotentReport(t *testing.T) {
	securityDB(t)
	hub := agenthub.New()
	for _, typ := range []string{proto.CmdApply, proto.CmdStop, proto.CmdStart, proto.CmdRestart, proto.CmdRollback, proto.CmdUnassign} {
		cmd := model.AgentCommand{AgentID: 1, Type: typ, TargetType: proto.TargetNode, TargetID: 1}
		if err := database.DB.Create(&cmd).Error; err != nil {
			t.Fatal(err)
		}
		first, err := claimCommand(1, cmd.ID)
		if err != nil || first == nil {
			t.Fatalf("未领取 %s: %v", typ, err)
		}
		if again, _ := claimCommand(1, 0); again != nil {
			t.Fatal("租约未到期重复领取")
		}
		database.DB.Model(&cmd).Update("sent_at", time.Now().Add(-time.Hour))
		retry, err := claimCommand(1, 0)
		if err != nil || retry == nil || retry.ID != cmd.ID {
			t.Fatalf("命令不可恢复 %s: %v", typ, err)
		}
		if err := finishCommand(hub, &cmd, proto.ResultData{OK: true, Message: "first"}); err != nil {
			t.Fatal(err)
		}
		if err := finishCommand(hub, &cmd, proto.ResultData{Message: "duplicate"}); err != nil {
			t.Fatal(err)
		}
		var saved model.AgentCommand
		database.DB.First(&saved, cmd.ID)
		if saved.Status != "done" || saved.Result != "first" {
			t.Fatal("重复报告覆盖终态")
		}
	}
	one := model.AgentCommand{AgentID: 1, Type: proto.CmdStop}
	two := model.AgentCommand{AgentID: 1, Type: proto.CmdStart}
	database.DB.Create(&one)
	database.DB.Create(&two)
	if cmd, _ := claimCommand(1, two.ID); cmd != nil {
		t.Fatal("后序命令越过前序")
	}
}

func TestConcurrentSnapshotsHaveUniqueVersions(t *testing.T) {
	securityDB(t)
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := recordConfigVersion(proto.TargetNode, 1, `{"v":1}`); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var versions []model.ConfigVersion
	database.DB.Order("version").Find(&versions)
	if len(versions) != 20 {
		t.Fatal(len(versions))
	}
	for i, v := range versions {
		if v.Version != i+1 {
			t.Fatal("重复或遗漏的版本")
		}
	}
	if err := database.DB.Create(&model.ConfigVersion{TargetType: proto.TargetNode, TargetID: 1, Version: 1}).Error; err == nil {
		t.Fatal("数据库没有唯一约束")
	}
}

func TestLogoutRevokesSessionAndLoginIsLimited(t *testing.T) {
	securityDB(t)
	h := NewAuthHandler(&config.Config{JWTSecret: "test-secret"})
	r := ginRouter()
	r.POST("/api/init", h.Init)
	r.POST("/api/auth/login", h.Login)
	r.POST("/api/auth/logout", middleware.JWT("test-secret"), h.Logout)
	r.GET("/api/auth/profile", middleware.JWT("test-secret"), h.Profile)
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/init", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
	login := httptest.NewRecorder()
	r.ServeHTTP(login, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"admin","password":"good-password"}`)))
	var out struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(login.Body.Bytes(), &out); err != nil || out.Token == "" {
		t.Fatal(login.Body.String())
	}
	call := func(method, path string) int {
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+out.Token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if call("GET", "/api/auth/profile") != 200 || call("POST", "/api/auth/logout") != 200 || call("GET", "/api/auth/profile") != 401 {
		t.Fatal("会话撤销未生效")
	}
	loginMu.Lock()
	loginAttempts = map[string]loginAttempt{}
	loginMu.Unlock()
	for i := 0; i < 11; i++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"absent","password":"wrong"}`)))
		if i == 10 && w.Code != 429 {
			t.Fatal("登录未限速")
		}
	}
}

func TestInvalidIDsAndSecretSerialization(t *testing.T) {
	r := ginRouter()
	r.GET("/api/agents/:id", func(c *gin.Context) { c.JSON(200, gin.H{}) })
	for _, id := range []string{"0", "-1", "1=1", "abc", "4294967296"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", "/api/agents/"+id, nil))
		if w.Code != 400 {
			t.Fatalf("%s 返回 %d", id, w.Code)
		}
	}
	b, _ := json.Marshal(model.Agent{Secret: "never-serialize"})
	if strings.Contains(string(b), "never-serialize") || strings.Contains(string(b), "secret") {
		t.Fatal("模型泄漏密钥")
	}
}

func ginRouter() *gin.Engine { r := gin.New(); r.Use(middleware.Security()); return r }
