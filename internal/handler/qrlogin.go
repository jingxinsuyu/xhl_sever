package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	DeviceID string `json:"device_id"`                   // MD5(设备码)，防 token 复制到其他设备
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
	// 计费校验：按【项目生效模式】判断（默认 membership = v1 行为，完全不变；
	// 只有项目显式配成 per_call/credits 时才走额度校验）
	ent := getEntitlement(claims.UserID, qrLoginProjectID)
	mode := projectMode(qrLoginProjectID, ent)
	unit := projectUnit(qrLoginProjectID)
	if mode == model.BillingMembership {
		if ent == nil || !ent.IsValid(timeNow()) {
			util.Fail(c, util.CodeNoPermission, "无该项目权限")
			return
		}
	} else if ent == nil || !ent.CanConsume(mode, unit, timeNow()) {
		util.Fail(c, util.CodeQuotaExhausted, modeLabel(mode)+"额度不足")
		return
	}

	// device_id 绑定比对：请求的 device_id 必须等于 MD5(绑定设备码)，防 token 复制到其他设备
	var binding model.UserBinding
	if err := database.DB.
		Where("user_id = ? AND project_id = ?", claims.UserID, qrLoginProjectID).
		First(&binding).Error; err != nil {
		util.Fail(c, util.CodeDeviceNotBound, "该账号尚未绑定设备，请重新登录")
		return
	}
	if req.DeviceID == "" || util.Md5Hex(binding.MachineCode) != req.DeviceID {
		util.Fail(c, util.CodeDeviceNotBound, "该账号已在其他设备登录，请先解绑后重试")
		return
	}

	// 每日调用计数（按接口），用于：CallLimit 超限拦截 + 每 25 次触发滑动验证码
	dailyCount := h.recordDailyCall(qrLoginProjectID, claims.UserID, "qrlogin")
	if project.CallLimit > 0 && dailyCount > int64(project.CallLimit) {
		util.Fail(c, util.CodeCallLimitExceed, "已达到每日使用上限")
		return
	}
	// 按配置步进触发滑动拼图验证码：达到步进点后须验证通过（区间凭证，本区间内放行）
	if h.captchaRequired("qrlogin", claims.UserID, dailyCount) {
		util.Fail(c, util.CodeCaptchaRequired, "请先完成拼图验证")
		return
	}

	// 扣减额度：per_call 扣次数、credits 扣积分、membership 只累计调用次数（调用即扣）
	balanceAfter, _ := consumeQuota(claims.UserID, qrLoginProjectID, mode, unit)
	// 扣费流水：只有真的动了额度才记一条（membership 模式不记）
	if bk := billingKindOf(mode); bk != "" {
		h.logBilling(BillingEntry{
			ProjectID: qrLoginProjectID, UserID: claims.UserID, Username: claims.Username,
			Kind: bk, Delta: -unit, BalanceAfter: balanceAfter,
			Reason: model.BillReasonCall, Remark: "扫码确认",
		})
	}

	// 开启 SSE 流
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	sendLog := func(msg string) { writeSSE(c, map[string]interface{}{"type": "log", "message": msg}) }

	// 调用记录：进到这一步才算"真的发起了一次调用"（前面的鉴权/额度/绑定/限流拒绝不算），
	// 成功失败都记一条，在函数返回时统一落库（失败也只忽略，绝不影响扫码）。
	started := time.Now()
	var logOk bool
	var logErrno, logMsg string
	defer func() {
		h.logCall(c, CallLogEntry{
			ProjectID: qrLoginProjectID, Action: model.CallActionQrLogin, Source: model.CallSourceUser,
			UserID: claims.UserID, Username: claims.Username,
			Ok: logOk, Errno: logErrno, Message: logMsg,
			CostKind: billingKindOf(mode), Cost: func() int {
				if billingKindOf(mode) == "" {
					return 0
				}
				return unit
			}(),
			Started: started,
		})
	}()

	sendResult := func(ok bool, errno, msg string) {
		logOk, logErrno, logMsg = ok, errno, msg
		writeSSE(c, map[string]interface{}{"type": "result", "ok": ok, "errno": errno, "message": msg})
	}

	sendLog("正在解析二维码")
	username, password, cookie, err := decryptAccountData(req.Data, h.Config.Security.QrLoginAESKey, h.Config.Security.ClientAESKey)
	if err != nil {
		sendResult(false, "", "账号数据解密失败："+err.Error())
		return
	}

	// 代理：按项目配置解析（inherit=全局默认 / none / fixed / pool）
	proxyAddr, proxyFail := h.projectProxy(qrLoginProjectID)
	if proxyFail {
		sendResult(false, "", "加载网络环境失败1000")
		return
	}

	// 扫码登录保护预检：cookie 账号开保护直接拦截（检测异常 fail-open，不误伤）
	// sendLog("正在检测账号状态")
	// if qrlogin.DetectScanProtect(cookie, proxyAddr) {
	// 	sendResult(false, "", qrlogin.ScanProtectBlockedMessage)
	// 	return
	// }

	// sendLog("正在生成环境信息")

	sendLog("正在登录")
	res, err := qrlogin.Confirm(req.LoginURL, cookie, proxyAddr)
	if err != nil {
		sendResult(false, "", "登录失败："+err.Error())
		return
	}
	if res.OK {
		h.saveCkData(claims.UserID, qrLoginProjectID, 0, username, password, cookie, "用户:"+claims.Username) // 登录成功才入表，来源=用户
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
// projectID / apiKeyID 仅用于后台按项目筛选和对账（用户端 apiKeyID=0，老数据两者为空）。
func (h *Handler) saveCkData(userID uint64, projectID string, apiKeyID uint64, username, password, cookie, source string) {
	var ck model.CkData
	err := database.DB.Where("user_id = ? AND username = ?", userID, username).First(&ck).Error
	if err == nil {
		updates := map[string]any{
			"password": password,
			"cookie":   cookie,
			"source":   source,
		}
		// 只在能确定归属时回填，避免把已有的项目归属覆盖成空
		if projectID != "" {
			updates["project_id"] = projectID
		}
		if apiKeyID != 0 {
			updates["api_key_id"] = apiKeyID
		}
		_ = database.DB.Model(&ck).Updates(updates).Error
		return
	}
	_ = database.DB.Create(&model.CkData{
		UserID:    userID,
		ProjectID: projectID,
		ApiKeyID:  apiKeyID,
		Username:  username,
		Password:  password,
		Cookie:    cookie,
		Source:    source,
	}).Error
}
