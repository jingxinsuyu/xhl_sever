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
	"gorm.io/gorm"
)

// fdevProjectID 「fdev签发服务」项目 id（开放平台 API Key 按项目绑定）。
const fdevProjectID = "100004"

const (
	fdevReqKeyPrefix   = "fdev:req:" // handle -> {fb, xyus, key_id}
	fdevReqTTL         = 5 * time.Minute // handle 有效期（请求包应 ≤15s 内发出）
	fdevSendWithinSecs = 15              // 请求包内嵌时间戳，提示调用方尽快发出
)

// fdevRequestPayload Redis 里暂存的 FB（= f(dev)，仅服务端保留，永不下发/不写日志）。
type fdevRequestPayload struct {
	FB    string `json:"fb"`     // base64(f(dev))
	XYUS  string `json:"xyus"`   // 设备串
	KeyID uint64 `json:"key_id"` // 绑定的 API Key id：只有创建它的 key 才能来解密
}

// OpenFdevIssueRequest 出包入参：给 android_id+uuid，或直接给算好的 xyus。
type OpenFdevIssueRequest struct {
	AndroidID string `json:"android_id"`
	UUID      string `json:"uuid"`
	XYUS      string `json:"xyus"`
}

// OpenFdevIssue 开放平台：生成 sofire z_id 签发的**加密请求包**（服务端只出包）。
//
// 返回 URL / BodyB64 / Headers，由调用方用**本地代理**发出；服务端不代为请求 sofire。
// `FB`（= f(dev)）只留在服务端 Redis（按 handle，绑定创建它的 API Key），永不下发、不写日志。
//
//	POST /api/open/fdev/issue     header: xhlkey
//	{ "android_id": "…", "uuid": "…" }   或   { "xyus": "32位大写hex|0" }
func (h *Handler) OpenFdevIssue(c *gin.Context) {
	ak, ok := h.fdevRequireKey(c)
	if !ok {
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

	// 扣费：出包调用即扣（config.cost.fdev_issue_cost，默认 1 积分/次）；解密接口 /open 不扣费。
	cost := h.fdevIssueCost()
	if cost > 0 {
		res := database.DB.Model(&model.ApiKey{}).
			Where("`key` = ? AND balance >= ?", ak.Key, cost).
			Update("balance", gorm.Expr("balance - ?", cost))
		if res.Error != nil {
			util.Fail(c, util.CodeDBError, "扣费失败")
			return
		}
		if res.RowsAffected == 0 {
			util.Fail(c, util.CodeInsufficientBalance, "积分不足")
			return
		}
		// 重新读取扣费后余额返回给调用方
		database.DB.First(ak, ak.ID)
	}

	handle, err := fdevNewHandle()
	if err != nil {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}
	if err := h.fdevSaveRequest(handle, fr.FB, fr.XYUS, ak.ID); err != nil {
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
		"cost":                cost,
		"balance":             ak.Balance,
	})
}

// OpenFdevOpenRequest 服务端解密入参。
type OpenFdevOpenRequest struct {
	Handle      string `json:"handle"`       // /issue 返回的 handle
	ResponseB64 string `json:"response_b64"` // sofire 响应的 base64（原文字节）
	Response    string `json:"response"`     // 兼容写法：直接给响应原文
}

// OpenFdevOpen 开放平台：由服务端解密 sofire 响应并直接返回签发结果。
//
// 调用方把 sofire 的**响应原文**回传，服务端用只存在于服务端的 FB 解密
// （AES-128-CBC 零 IV → 去 PKCS7 → 可能 gunzip → 合并多个 JSON），返回 token/st/nt。
// FB 与请求明文**都不下发**；handle 与创建它的 API Key 绑定，解密成功后即失效。
//
//	POST /api/open/fdev/open      header: xhlkey
//	{ "handle": "…", "response_b64": "…" }
func (h *Handler) OpenFdevOpen(c *gin.Context) {
	ak, ok := h.fdevRequireKey(c)
	if !ok {
		return
	}

	var req OpenFdevOpenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：handle 不能为空")
		return
	}
	req.Handle = strings.TrimSpace(req.Handle)
	rawB64 := strings.TrimSpace(req.ResponseB64)
	if rawB64 == "" {
		rawB64 = strings.TrimSpace(req.Response)
	}
	if req.Handle == "" || rawB64 == "" {
		util.Fail(c, util.CodeParamError, "参数错误：handle、response_b64 不能为空")
		return
	}

	payload, err := h.fdevLoadRequest(req.Handle)
	if err != nil {
		util.Fail(c, util.CodeNotFound, "handle 不存在或已过期")
		return
	}
	if payload.KeyID != ak.ID {
		util.Fail(c, util.CodeForbidden, "该 handle 不属于当前 API Key")
		return
	}
	fb, err := base64.StdEncoding.DecodeString(payload.FB)
	if err != nil || len(fb) != 16 {
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	// 响应原文：优先按 base64 解，解不开就当原文用（对非 base64 文本友好）
	respBody, err := base64.StdEncoding.DecodeString(rawB64)
	if err != nil {
		respBody = []byte(rawB64)
	}

	tok, err := fdev.OpenResponse(respBody, fb)
	if err != nil {
		util.Fail(c, util.CodeParamError, "解密失败："+err.Error())
		return
	}

	// 解密成功后 handle 立即失效（一次性）
	h.fdevDeleteRequest(req.Handle)

	util.OK(c, gin.H{
		"token":       tok.Token,
		"st":          tok.ST,
		"nt":          tok.NT,
		"valid":       tok.Valid(),
		"token_bytes": tok.TokenBytes(),
		"xyus":        payload.XYUS,
	})
}

// ---------------------------------------------------------------- 内部工具

// fdevIssueCost 返回 fdev 出包每次的扣费积分（config.cost.fdev_issue_cost，0=不扣）。
// 解密接口 /open 不扣费。生产配置里已设 1。
func (h *Handler) fdevIssueCost() int {
	if h.Config.Cost.FdevIssueCost > 0 {
		return h.Config.Cost.FdevIssueCost
	}
	return 0
}

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

// fdevSaveRequest 把 FB 暂存到 Redis（按 handle，绑定 API Key id，TTL 5 分钟）。
func (h *Handler) fdevSaveRequest(handle string, fb []byte, xyus string, keyID uint64) error {
	if h.Redis == nil {
		return errors.New("redis 未就绪")
	}
	payload, err := json.Marshal(fdevRequestPayload{
		FB:    base64.StdEncoding.EncodeToString(fb),
		XYUS:  xyus,
		KeyID: keyID,
	})
	if err != nil {
		return err
	}
	return h.Redis.Set(context.Background(), fdevReqKeyPrefix+handle, payload, fdevReqTTL).Err()
}

// fdevLoadRequest 取出 handle 对应的暂存数据。
func (h *Handler) fdevLoadRequest(handle string) (*fdevRequestPayload, error) {
	if h.Redis == nil {
		return nil, errors.New("redis 未就绪")
	}
	raw, err := h.Redis.Get(context.Background(), fdevReqKeyPrefix+handle).Bytes()
	if err != nil {
		return nil, err
	}
	var p fdevRequestPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.FB == "" {
		return nil, errors.New("payload 异常")
	}
	return &p, nil
}

// fdevDeleteRequest 让 handle 立即失效（一次性）。
func (h *Handler) fdevDeleteRequest(handle string) {
	if h.Redis == nil {
		return
	}
	_ = h.Redis.Del(context.Background(), fdevReqKeyPrefix+handle).Err()
}

// fdevNewHandle 生成一次性的 handle（16 字节随机 hex）。
func fdevNewHandle() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
