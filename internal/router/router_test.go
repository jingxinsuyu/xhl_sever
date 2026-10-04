package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// setupWeb 造一个最小的前端产物目录：index.html + assets/app.js + favicon.ico。
func setupWeb(t *testing.T) string {
	t.Helper()
	webDir := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatalf("准备静态文件失败: %v", err)
		}
	}
	must(os.WriteFile(filepath.Join(webDir, "index.html"), []byte("<html>admin-app</html>"), 0o644))
	must(os.WriteFile(filepath.Join(webDir, "favicon.ico"), []byte("ico"), 0o644))
	must(os.MkdirAll(filepath.Join(webDir, "assets"), 0o755))
	must(os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte("console.log(1)"), 0o644))
	return webDir
}

// TestAdminPathHidesAdmin 验证配置前缀后：只有前缀下能进后台，根路径等一律 404。
func TestAdminPathHidesAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	webDir := setupWeb(t)
	const prefix = "/ef16e15c12bb"

	r := gin.New()
	registerAdminFiles(r, webDir, prefix)

	cases := []struct {
		path string
		want int
	}{
		// 未带前缀：全部 404（后台不暴露）
		{"/", http.StatusNotFound},
		{"/index.html", http.StatusNotFound},
		{"/admin", http.StatusNotFound},
		{"/apikeys", http.StatusNotFound},
		{"/assets/app.js", http.StatusNotFound},
		// 带前缀：正常访问
		{prefix, http.StatusFound}, // 302 → 补末尾斜杠
		{prefix + "/", http.StatusOK},
		{prefix + "/projects", http.StatusOK},       // SPA 深链回退 index.html
		{prefix + "/assets/app.js", http.StatusOK},  // 静态资源
		{prefix + "/favicon.ico", http.StatusOK},    // 图标
		{prefix + "/index.html", http.StatusMovedPermanently}, // http.ServeFile 会把 .../index.html 301 到 .../
		// /api、/uploads 保持 404 JSON（不进 SPA）
		{"/api/health", http.StatusNotFound},
		{"/uploads/x.png", http.StatusNotFound},
	}
	for _, c := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		r.ServeHTTP(w, req)
		if w.Code != c.want {
			t.Errorf("path %q: got %d want %d", c.path, w.Code, c.want)
		}
	}

	// 前缀根路径返回的是 index.html 内容，且必须禁用缓存（否则发新版后浏览器仍跑旧前端）
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, prefix+"/", nil))
	if body := w.Body.String(); body != "<html>admin-app</html>" {
		t.Errorf("前缀根路径内容 = %q, 期望 index.html", body)
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
		t.Errorf("index.html Cache-Control = %q, 期望包含 no-store", cc)
	}
}

// TestNoAdminPathKeepsRoot 验证未配置前缀时保持旧行为（根路径可用，便于本地调试）。
func TestNoAdminPathKeepsRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	webDir := setupWeb(t)

	r := gin.New()
	registerAdminFiles(r, webDir, "")

	for _, p := range []string{"/", "/projects", "/assets/app.js"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusOK {
			t.Errorf("path %q: got %d want 200", p, w.Code)
		}
	}

	// /api 前缀仍返回 404
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("/api/health: got %d want 404", w.Code)
	}
}
