package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"dfpanel/internal/config"
	"dfpanel/internal/database"
	"dfpanel/internal/middleware"
	"dfpanel/internal/model"
)

func passwordTestRouter(t *testing.T) (*gin.Engine, *AuthHandler, model.User, string) {
	t.Helper()
	securityDB(t)
	loginMu.Lock()
	loginAttempts = map[string]loginAttempt{}
	loginMu.Unlock()
	t.Cleanup(func() {
		loginMu.Lock()
		loginAttempts = map[string]loginAttempt{}
		loginMu.Unlock()
	})
	h := NewAuthHandler(&config.Config{JWTSecret: "password-test-secret"})
	r := ginRouter()
	r.POST("/api/init", h.Init)
	r.POST("/api/auth/login", h.Login)
	r.POST("/api/auth/password", middleware.JWT(h.cfg.JWTSecret), h.ChangePassword)
	r.GET("/api/auth/profile", middleware.JWT(h.cfg.JWTSecret), h.Profile)
	w := passwordRequest(r, http.MethodPost, "/api/init", "", `{"username":"admin","password":"old-password"}`)
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	var user model.User
	if err := database.DB.Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	token := passwordLogin(t, r, "old-password")
	return r, h, user, token
}

func passwordRequest(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func passwordLogin(t *testing.T, r *gin.Engine, password string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": password})
	w := passwordRequest(r, http.MethodPost, "/api/auth/login", "", string(body))
	var out struct {
		Token string `json:"token"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Token == "" {
		t.Fatalf("登录失败 status=%d body=%s", w.Code, w.Body.String())
	}
	return out.Token
}

func TestChangePasswordRejectsInvalidInputWithoutRevokingSession(t *testing.T) {
	r, _, original, token := passwordTestRouter(t)
	for _, tc := range []struct {
		name    string
		current string
		new     string
	}{
		{"missing current", "", "new-password"},
		{"wrong current", "incorrect", "new-password"},
		{"too short", "old-password", "short"},
		{"too long", "old-password", strings.Repeat("a", 65)},
		{"bcrypt byte limit", "old-password", strings.Repeat("中", 25)},
		{"unchanged", "old-password", "old-password"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]string{"currentPassword": tc.current, "newPassword": tc.new})
			w := passwordRequest(r, http.MethodPost, "/api/auth/password", token, string(body))
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
	w := passwordRequest(r, http.MethodPost, "/api/auth/password", "", `{"currentPassword":"old-password","newPassword":"new-password"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录也能修改密码: %d", w.Code)
	}
	var saved model.User
	if err := database.DB.First(&saved, original.ID).Error; err != nil || saved.PasswordHash != original.PasswordHash {
		t.Fatalf("失败请求修改了密码: %v", err)
	}
	if passwordRequest(r, http.MethodGet, "/api/auth/profile", token, "").Code != http.StatusOK {
		t.Fatal("输入错误撤销了有效会话")
	}
	if err := database.DB.Model(&saved).Update("role", "user").Error; err != nil {
		t.Fatal(err)
	}
	w = passwordRequest(r, http.MethodPost, "/api/auth/password", token, `{"currentPassword":"old-password","newPassword":"new-password"}`)
	if w.Code != http.StatusForbidden {
		t.Fatalf("非管理员也能修改密码: %d", w.Code)
	}
}

func TestChangePasswordRevokesAllDevicesAndRejectsStaleLogin(t *testing.T) {
	r, h, original, token := passwordTestRouter(t)
	secondToken := passwordLogin(t, r, "old-password")
	other := model.User{Username: "other", PasswordHash: original.PasswordHash, Role: "admin"}
	if err := database.DB.Create(&other).Error; err != nil {
		t.Fatal(err)
	}
	otherToken, err := h.createLoginSession(other)
	if err != nil {
		t.Fatal(err)
	}
	w := passwordRequest(r, http.MethodPost, "/api/auth/password", token, `{"currentPassword":"old-password","newPassword":"new-password"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	for _, oldToken := range []string{token, secondToken} {
		if passwordRequest(r, http.MethodGet, "/api/auth/profile", oldToken, "").Code != http.StatusUnauthorized {
			t.Fatal("改密码后旧设备会话仍有效")
		}
	}
	if passwordRequest(r, http.MethodGet, "/api/auth/profile", otherToken, "").Code != http.StatusOK {
		t.Fatal("修改一个账号的密码撤销了其他账号会话")
	}
	// 模拟旧密码已通过 bcrypt 校验，但改密码事务先完成的并发登录。
	if _, err := h.createLoginSession(original); !errors.Is(err, errPasswordChanged) {
		t.Fatalf("旧密码校验结果仍可创建会话: %v", err)
	}
	var saved model.User
	if err := database.DB.First(&saved, original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.PasswordHash == "new-password" || bcrypt.CompareHashAndPassword([]byte(saved.PasswordHash), []byte("new-password")) != nil {
		t.Fatal("新密码未正确加密保存")
	}
	w = passwordRequest(r, http.MethodPost, "/api/auth/login", "", `{"username":"admin","password":"old-password"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("旧密码仍能登录")
	}
	newToken := passwordLogin(t, r, "new-password")
	if passwordRequest(r, http.MethodGet, "/api/auth/profile", newToken, "").Code != http.StatusOK {
		t.Fatal("新密码登录后会话无效")
	}
}

func TestChangePasswordRollsBackWhenSessionRevocationFails(t *testing.T) {
	r, _, original, token := passwordTestRouter(t)
	if err := database.DB.Exec(`CREATE TRIGGER fail_session_revoke BEFORE DELETE ON auth_sessions BEGIN SELECT RAISE(ABORT, 'revoke failed'); END`).Error; err != nil {
		t.Fatal(err)
	}
	w := passwordRequest(r, http.MethodPost, "/api/auth/password", token, `{"currentPassword":"old-password","newPassword":"new-password"}`)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var saved model.User
	if err := database.DB.First(&saved, original.ID).Error; err != nil || saved.PasswordHash != original.PasswordHash {
		t.Fatalf("会话撤销失败时密码未回滚: %v", err)
	}
	if passwordRequest(r, http.MethodGet, "/api/auth/profile", token, "").Code != http.StatusOK {
		t.Fatal("失败事务撤销了原会话")
	}
}

func TestChangePasswordLimitsCurrentPasswordGuesses(t *testing.T) {
	r, _, _, token := passwordTestRouter(t)
	for i := 0; i < 11; i++ {
		w := passwordRequest(r, http.MethodPost, "/api/auth/password", token, `{"currentPassword":"incorrect","newPassword":"new-password"}`)
		want := http.StatusBadRequest
		if i == 10 {
			want = http.StatusTooManyRequests
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("限速响应没有 Retry-After")
			}
		}
		if w.Code != want {
			t.Fatalf("第 %d 次猜测 status=%d want=%d", i+1, w.Code, want)
		}
	}
	if passwordRequest(r, http.MethodGet, "/api/auth/profile", token, "").Code != http.StatusOK {
		t.Fatal("修改密码限速影响了有效登录")
	}
}

func TestChangePasswordRejectsSessionRevokedAfterAuthentication(t *testing.T) {
	_, h, original, token := passwordTestRouter(t)
	r := ginRouter()
	r.POST("/api/auth/password", middleware.JWT(h.cfg.JWTSecret), func(c *gin.Context) {
		if err := database.DB.Where("user_id = ?", original.ID).Delete(&model.AuthSession{}).Error; err != nil {
			t.Fatal(err)
		}
		c.Next()
	}, h.ChangePassword)
	w := passwordRequest(r, http.MethodPost, "/api/auth/password", token, `{"currentPassword":"old-password","newPassword":"new-password"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("已撤销的请求仍可修改密码 status=%d body=%s", w.Code, w.Body.String())
	}
	var saved model.User
	if err := database.DB.First(&saved, original.ID).Error; err != nil || saved.PasswordHash != original.PasswordHash {
		t.Fatalf("已撤销的请求覆盖了密码: %v", err)
	}
}
