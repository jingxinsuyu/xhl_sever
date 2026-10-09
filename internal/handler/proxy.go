package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xhl-server/internal/model"
	"xhl-server/internal/projsetting"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// proxyURLRedisKey 代理池提取地址在 Redis 中的 key。
const proxyURLRedisKey = "xhl:proxy_url"

// ProxyConfigRequest 代理池配置保存请求
type ProxyConfigRequest struct {
	ProxyURL string `json:"proxy_url"` // 提取代理的地址（空 = 不走代理）
}

// GetProxyConfig 读取代理池配置（所有项目公共）。
func (h *Handler) GetProxyConfig(c *gin.Context) {
	util.OK(c, gin.H{"proxy_url": h.proxyURL()})
}

// SaveProxyConfig 保存代理池配置（写入 Redis，低频读，AOF 持久化）。
func (h *Handler) SaveProxyConfig(c *gin.Context) {
	var req ProxyConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：proxy_url 不能为空")
		return
	}
	req.ProxyURL = strings.TrimSpace(req.ProxyURL)
	old := h.proxyURL()
	if err := h.Redis.Set(context.Background(), proxyURLRedisKey, req.ProxyURL, 0).Err(); err != nil {
		util.Fail(c, util.CodeDBError, "保存失败")
		return
	}
	h.recordAudit(c, model.AuditProxyConfig, "", "proxy:global",
		gin.H{"from": old, "to": req.ProxyURL})
	util.OK(c, gin.H{"proxy_url": req.ProxyURL})
}

// proxyURL 读取代理池提取地址（未配置返回空）。
func (h *Handler) proxyURL() string {
	v, err := h.Redis.Get(context.Background(), proxyURLRedisKey).Result()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

// fetchProxyFrom 从指定提取地址取一个代理（空地址返回空串）。
// 响应取首行，支持 host:port / http://host:port / host:port:user:pass 三种格式。
func fetchProxyFrom(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(u)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return normalizeProxy(firstLine(string(body)))
}

// projectProxy 解析「指定项目」实际使用的代理地址（v2：按项目独立配置）。
//
// 优先级：项目配置 project_setting.proxy_mode → 全局默认（现有 Redis 值）→ 不用代理。
//   - inherit（默认）：沿用全局那一个 = v1 行为，保证升级后完全一致
//   - none：该项目直连
//   - fixed：用项目里配的固定地址
//   - pool：用项目里配的提取接口地址
//
// failed=true 表示「该项目配了代理但没取到」——调用方据此返回"加载网络环境失败1000"，
// 与 v1「配了代理就不允许静默降级直连」的语义一致。
func (h *Handler) projectProxy(projectID string) (addr string, failed bool) {
	mode := model.ProxyInherit
	u := ""
	if s, err := projsetting.Get(projectID); err == nil && s != nil {
		mode = s.ProxyModeOrDefault()
		u = strings.TrimSpace(s.ProxyURL)
	}
	switch mode {
	case model.ProxyNone:
		return "", false
	case model.ProxyFixed:
		a := normalizeProxy(u)
		return a, a == ""
	case model.ProxyPool:
		a := fetchProxyFrom(u)
		return a, a == ""
	default: // inherit
		gu := h.proxyURL()
		if gu == "" {
			return "", false
		}
		a := fetchProxyFrom(gu)
		return a, a == ""
	}
}

// firstLine 取首个非空行。
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// normalizeProxy 规范代理地址：
//   - host:port            → http://host:port
//   - host:port:user:pass   → http://user:pass@host:port
//   - 已含 scheme（http://...）→ 原样
func normalizeProxy(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if strings.Contains(line, "://") {
		return line
	}
	parts := strings.Split(line, ":")
	if len(parts) == 4 {
		// host:port:user:pass
		return fmt.Sprintf("http://%s:%s@%s:%s", parts[2], parts[3], parts[0], parts[1])
	}
	return "http://" + line
}
