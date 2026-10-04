package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"xhl-server/internal/database"
	"xhl-server/internal/fdev"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// fdevProjectID 「fdev签发服务」项目 id（开放平台 API Key 按项目绑定）。
const fdevProjectID = "100004"

const (
	fdevReqKeyPrefix   = "fdev:req:"     // handle -> {fb, xyus}
	fdevReqTTL         = 10 * time.Minute // 出包后保留 FB 的时长（供 /rkey 用）
	fdevSendWithinSecs = 15               // 请求包内嵌时间戳，提示调用方尽快发出
)

// fdevRequestPayload Redis 里暂存的 FB（= f(dev)，仅服务端保留，不下发/不写日志）。
type fdevRequestPayload struct {
	FB   string `json:"fb"` // base64(f(dev))
	XYUS string `json:"xyus"`
}

// OpenFdevIssueRequest 出包入参：给 android_id+uuid，或直接给算好的 xyus。
type OpenFdevIssueRequest struct {
	AndroidID string `json:"android_id"`
	UUID      string `json:"uuid"`
	XYUS      string `json:"xyus"`
}

// OpenFdevIssue 开放平台「加密接口」：生成 sofire z_id 签发的**加密请求包**。
//
// 服务端只出包（部署姿势 B）：返回 URL / BodyB64 / Headers，由调用方用**本地代理**发出；
// 服务端**不**代为请求 sofire、**不**解密响应。`FB`（= f(dev)）只留在服务端 Redis，
// 按 handle 暂存，供后续 /rkey 使用；绝不下发、绝不写日志。
//
//	POST /api/open/fdev/issue     header: xhlkey
//	{ "android_id": "…", "uuid": "…" }   或   { "xyus": "32位大写hex|0" }
func (h *Handler) OpenFdevIssue(c *gin.Context) {
	if _, ok := h.fdevRequireKey(c); !ok {
		return
	}

	var req OpenFdevIssueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：需要 android_id+uuid 或 xyus")
		return
	}
	req.AndroidID = strings.TrimSpace(req.AndroidID)
	req.UUID = strings.TrimSpace(req.UUID)
	req.XYUS = strings.TrimSpace(req.XYUS)

	var (
		fr  *fdev.Request
		err error
	)
	switch {
	case req.XYUS != "":
		fr, err = fdev.BuildRequestXYUS(req.XYUS)
	case req.AndroidID != "" && req.UUID != "":
		fr, err = fdev.BuildRequest(fdev.Device{AndroidID: req.AndroidID, UUID: req.UUID})
	default:
		util.Fail(c, util.CodeParamError, "参数错误：需要 android_id+uuid 或 xyus")
		return
	}
	if err != nil {
		util.Fail(c, util.CodeParamError, "生成请求包失败："+err.Error())
		return
	}

	handle, err := fdevNewHandle()
	if err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if err := h.fdevSaveRequest(handle, fr.FB, fr.XYUS); err != nil {
		util.Fail(c, util.CodeDBError, "保存请求包失败")
		return
	}

	util.OK(c, gin.H{
		"handle":              handle,
		"url":                 fr.URL,
		"body_b64":            fr.BodyB64,
		"headers":             fr.Headers,
		"x_dev":               fr.XDev,
		"xyus":                fr.XYUS,
		"send_within_seconds": fdevSendWithinSecs,
	})
}

// OpenFdevRkeyRequest 解密辅助入参。
type OpenFdevRkeyRequest struct {
	Handle      string `json:"handle"`        // /issue 返回的 handle
	RespSkeyB64 string `json:"resp_skey_b64"` // 响应里的 skey（base64 原文）
	RespSkey    string `json:"resp_skey"`     // 兼容写法，同上
}

// OpenFdevRkey 开放平台「加密接口」：由 handle + 响应里的 skey 算出 rkey。
//
// rkey = resp_skey XOR FB；调用方拿到 rkey 后在本地解密（服务端不做 AES 解密）：
//
//	data = base64decode(响应.data) → AES-128-CBC(rkey, IV=全0) 解密 → 去 PKCS7
//	     → 明文可能是 gzip，若可 gunzip 则解压 → 里面可能拼了多个 JSON，逐个解析合并
//	     → 取 token / st / nt（st="56" 且 token 解码 65 字节才算通过）
//
//	POST /api/open/fdev/rkey      header: xhlkey
//	{ "handle": "…", "resp_skey_b64": "…" }
func (h *Handler) OpenFdevRkey(c *gin.Context) {
	if _, ok := h.fdevRequireKey(c); !ok {
		return
	}

	var req OpenFdevRkeyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：handle 不能为空")
		return
	}
	req.Handle = strings.TrimSpace(req.Handle)
	skeyB64 := strings.TrimSpace(req.RespSkeyB64)
	if skeyB64 == "" {
		skeyB64 = strings.TrimSpace(req.RespSkey)
	}
	if req.Handle == "" || skeyB64 == "" {
		util.Fail(c, util.CodeParamError, "参数错误：handle、resp_skey_b64 不能为空")
		return
	}

	fb, xyus, err := h.fdevLoadRequest(req.Handle)
	if err != nil {
		util.Fail(c, util.CodeNotFound, "handle 不存在或已过期")
		return
	}
	rskey, err := base64.StdEncoding.DecodeString(skeyB64)
	if err != nil || len(rskey) < 16 {
		util.Fail(c, util.CodeParamError, "resp_skey 不是有效的 base64（至少 16 字节）")
		return
	}

	rkey := make([]byte, 16)
	for i := 0; i < 16; i++ {
		rkey[i] = rskey[i] ^ fb[i]
	}

	util.OK(c, gin.H{
		"rkey_b64": base64.StdEncoding.EncodeToString(rkey),
		"xyus":     xyus,
	})
}

// ---------------------------------------------------------------- 内部工具

// fdevRequireKey 校验开放平台 API Key，并要求它属于「fdev签发服务」项目。
func (h *Handler) fdevRequireKey(c *gin.Context) (*model.ApiKey, bool) {
	ak := middleware.GetApiKey(c)
	if ak == nil {
		util.Fail(c, util.CodeUnauthorized, "xhlkey 无效")
		return nil, false
	}
	if ak.ProjectID != fdevProjectID {
		util.Fail(c, util.CodeForbidden, "仅项目 "+fdevProjectID+"（fdev签发服务）的 API 可调用")
		return nil, false
	}
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", fdevProjectID).First(&project).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return nil, false
	}
	return ak, true
}

// fdevSaveRequest 把 FB 暂存到 Redis（按 handle，TTL 10 分钟）。
func (h *Handler) fdevSaveRequest(handle string, fb []byte, xyus string) error {
	if h.Redis == nil {
		return errors.New("redis 未就绪")
	}
	payload, err := json.Marshal(fdevRequestPayload{
		FB:   base64.StdEncoding.EncodeToString(fb),
		XYUS: xyus,
	})
	if err != nil {
		return err
	}
	return h.Redis.Set(context.Background(), fdevReqKeyPrefix+handle, payload, fdevReqTTL).Err()
}

// fdevLoadRequest 取出 handle 对应的 FB。
func (h *Handler) fdevLoadRequest(handle string) ([]byte, string, error) {
	if h.Redis == nil {
		return nil, "", errors.New("redis 未就绪")
	}
	raw, err := h.Redis.Get(context.Background(), fdevReqKeyPrefix+handle).Bytes()
	if err != nil {
		return nil, "", err
	}
	var p fdevRequestPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, "", err
	}
	fb, err := base64.StdEncoding.DecodeString(p.FB)
	if err != nil {
		return nil, "", err
	}
	if len(fb) != 16 {
		return nil, "", errors.New("fb 长度异常")
	}
	return fb, p.XYUS, nil
}

// fdevNewHandle 生成一次性的 handle（16 字节随机 hex）。
func fdevNewHandle() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
