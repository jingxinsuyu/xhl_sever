package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"xhl-server/internal/baidu/qrlogin"
	"xhl-server/internal/crypto"
	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// qrLoginProjectID 需要会员身份的项目 id（「小火龙扫码登录器」= 100001）。
const qrLoginProjectID = "100001"

// QrLoginRequest qrlogin 请求
type QrLoginRequest struct {
	LoginURL string `json:"loginUrl" binding:"required"` // 前端识别二维码中的链接（含 sign）
	Data     string `json:"data" binding:"required"`     // AES 加密后 base64 的 用户名----密码----cookie
}

// QrLogin 百度扫码确认（SSE 流式）。
// 鉴权/会员/参数校验通过后返回 text/event-stream，逐步推送 log 事件，最后 result 事件结束；
// 校验失败在开流前直接返回普通 JSON 错误。
func (h *Handler) QrLogin(c *gin.Context) {
	claims := middleware.GetClaims(c)
	if claims == nil {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		return
	}
	var req QrLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误：loginUrl、data 不能为空")
		return
	}

	// 校验项目存在 + 用户是 100001 项目会员（未过期）
	var project model.Project
	if err := database.DB.Where("id = ? AND deleted_at IS NULL", qrLoginProjectID).First(&project).Error; err != nil {
		util.Fail(c, util.CodeNotFound, "项目不存在或已停用")
		return
	}
	ent := getEntitlement(claims.UserID, qrLoginProjectID)
	if ent == nil || !ent.IsValid(timeNow()) {
		util.Fail(c, util.CodeNoPermission, "无该项目权限")
		return
	}

	// 开启 SSE 流
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	sendLog := func(msg string) { writeSSE(c, map[string]interface{}{"type": "log", "message": msg}) }
	sendResult := func(ok bool, errno, msg string) {
		writeSSE(c, map[string]interface{}{"type": "result", "ok": ok, "errno": errno, "message": msg})
	}

	sendLog("正在解析二维码")
	username, password, cookie, err := decryptAccountData(req.Data, h.Config.Security.QrLoginAESKey, h.Config.Security.ClientAESKey)
	if err != nil {
		sendResult(false, "", "账号数据解密失败："+err.Error())
		return
	}

	// 代理池：未配置则不走代理
	proxyAddr := ""
	if h.proxyURL() != "" {
		// sendLog("正在加载网络环境")
		proxyAddr = h.fetchProxy()
		if proxyAddr == "" {
			sendResult(false, "", "加载网络环境失败1000")
			return
		}
	}

	// sendLog("正在生成环境信息")

	sendLog("正在登录")
	res, err := qrlogin.Confirm(req.LoginURL, cookie, proxyAddr)
	if err != nil {
		sendResult(false, "", "登录失败："+err.Error())
		return
	}
	if res.OK {
		h.saveCkData(claims.UserID, username, password, cookie, "用户:"+claims.Username) // 登录成功才入表，来源=用户
		sendResult(true, res.Errno, "确认成功")
		return
	}
	msg := res.Message
	if msg == "" {
		msg = "确认失败"
	}
	sendResult(false, res.Errno, msg)
}

// writeSSE 写一条 SSE 事件并刷新。
func writeSSE(c *gin.Context, payload map[string]interface{}) {
	data, _ := json.Marshal(payload)
	fmt.Fprintf(c.Writer, "data: %s\n\n", data)
	c.Writer.Flush()
}

// decryptAccountData 解密 qrlogin data：
// 明文 = 用户名----密码----cookie；优先单层 base64（AES-ECB-PKCS7），失败回退双重 base64。
func decryptAccountData(data, qrKey, clientKey string) (username, password, cookie string, err error) {
	key := strings.TrimSpace(qrKey)
	if key == "" {
		key = clientKey
	}
	var plain string
	if p, e := crypto.AesECBDecryptPKCS7Base64(data, key); e == nil {
		plain = p
	} else if p, e := crypto.AesDecryptDouble64(data, key); e == nil {
		plain = p
	} else {
		return "", "", "", errors.New("data 无法解密")
	}
	parts := strings.SplitN(plain, "----", 3)
	if len(parts) < 3 {
		return "", "", "", errors.New("data 格式错误，应为 用户名----密码----cookie")
	}
	return strings.TrimSpace(parts[0]), parts[1], strings.TrimSpace(parts[2]), nil
}

// saveCkData 保存用户百度账号凭证：同一用户名下更新（密码/cookie/source），新用户名插入。
// source 为来源标签：用户调用填「用户:用户名」，开放平台填「开放平台:key名」。
func (h *Handler) saveCkData(userID uint64, username, password, cookie, source string) {
	var ck model.CkData
	err := database.DB.Where("user_id = ? AND username = ?", userID, username).First(&ck).Error
	if err == nil {
		_ = database.DB.Model(&ck).Updates(map[string]any{
			"password": password,
			"cookie":   cookie,
			"source":   source,
		}).Error
		return
	}
	_ = database.DB.Create(&model.CkData{
		UserID:   userID,
		Username: username,
		Password: password,
		Cookie:   cookie,
		Source:   source,
	}).Error
}
