// 「登录记录查询」授权 / 计费接口（客户机本地执行 + 服务端只授权与计费）。
//
// ★ 架构（重要，不要改成服务端代理）：**服务端不产生任何上游流量**。
//
//	上游是百度 passport 的 loginrecord 接口，服务端出口 IP 集中调用会被风控封掉；
//	所以真正的 HTTP 请求必须由**客户端用本机 IP** 发出。服务端只做三件事：
//	  1. POST /api/xhl/login-records/prepare —— 鉴权 + 额度预检 + 下发一张
//	     「短时效 + 一次性 nonce + 绑定用户/项目/设备码」的**授权票据**（HMAC 签名）；
//	     票据里带**上游请求的拼装指令**（method / URL / query 顺序 / headers / 占位符）
//	     与**响应解析规则**（code 字段、成功码、记录路径、字段名、错误码释义）。
//	  2. POST /api/xhl/login-records/report —— 客户端回报执行结果：校验票据签名/有效期/归属
//	     与 nonce 未被用过；**成功才扣费**并写 billing_log，成功失败都写 call_log。
//	  3. 协议、域名、参数、解析规则全部来自 config.yaml 的 login_records 段：
//	     上游变了只改配置重启，老客户端不用重新发版。
//
// 诚实边界（必须写进文档，不要假装能防）：
//   - 客户端既然要直连上游，本机就能拿到上游数据；票据只能挡「重放、跨设备盗用、
//     无票据白嫖」，挡不住「已授权客户端谎报失败」（报了失败只是它自己少扣一次费）。
//   - 因此这里给的防线是：一次性 nonce + 短时效 + 用户/项目/设备码绑定 + 项目日调用限额
//     （/prepare 走 recordDailyCall，超限返回 1019）+ call_log 全量留痕（便于事后告警）。
package handler

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/projsetting"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// loginRecordsProjectID 「小火龙登录记录查询」项目 id（用户 token 无 pid 时的兜底）。
const loginRecordsProjectID = "100006"

// 默认协议参数（照 小火龙账号管理器v2/baidu/accountcheck/check.go 的抓包实现）。
const (
	loginRecordsDefaultURL = "https://passport.baidu.com/v3/api/user/loginrecord"
	loginRecordsDefaultUA  = "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/69.0.3497.100 Safari/537.36"
	loginRecordsDefaultCT   = "application/x-javascript;charset=utf-8"
	loginRecordsMaxCookie   = 4096
	loginRecordsTicketVer   = 1
	loginRecordsNoncePrefix = "xhl:login-records:used:"
	// 占位符：由客户端在发送请求时替换（服务端不下发 ck 原串，只下发模板）。
	phCK      = "${CK}"
	phTSMilli = "${TS_MS}"
	phUpKey   = "${UPSTREAM_KEY}"
)

// loginRecordsErrorCodes 上游错误码释义（随票据下发，客户端不必内置这套表）。
// 与 小火龙账号管理器v2/baidu/accountcheck/check.go 的 PassportCode* 一致。
var loginRecordsErrorCodes = map[string]string{
	"400021": "BDUSS 已过期/登录状态失效（账号离线）",
	"50000":  "触发风控",
	"160102": "BDUSS 为空",
	"12":     "用户信息不存在",
	"-202":   "用户信息获取失败",
}

// loginRecordsTicket 票据载荷（整体经 HMAC-SHA256 签名，客户端不得改动）。
// 注意：绝不把 ck 原文放进票据，只放 sha256(ck) 指纹，票据被截获也无法还原 ck。
type loginRecordsTicket struct {
	Ver         int    `json:"ver"`
	Nonce       string `json:"nonce"`
	UserID      uint64 `json:"uid"`
	Username    string `json:"username"`
	ProjectID   string `json:"pid"`
	MachineCode string `json:"machine_code"`
	CkHash      string `json:"ck_hash"`
	Action      string `json:"action"`
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
}

// loginRecordsPlan 归一化后的协议计划（config 覆盖内置默认）。
type loginRecordsPlan struct {
	method  string
	url     string
	query   map[string]string
	headers map[string]string
	timeout int
	success []int
	ttl     int
	secret  string
}

// loginRecordsPlanOf 读取 config.yaml login_records 段并归一化（缺项一律回退内置默认）。
func (h *Handler) loginRecordsPlanOf() loginRecordsPlan {
	c := h.Config.LoginRecords

	p := loginRecordsPlan{
		method: "GET",
		url:    loginRecordsDefaultURL,
		query: map[string]string{
			"client":     "pc",
			"clientfrom": "pc",
			"lang":       "zh-cn",
			"tpl":        "pp",
			"ttt":        phTSMilli, // 毫秒时间戳防缓存，由客户端发送时生成
		},
		headers: map[string]string{
			"Cookie":       phCK,
			"User-Agent":   loginRecordsDefaultUA,
			"Content-Type": loginRecordsDefaultCT,
		},
		timeout: 15,
		success: []int{110000, 0},
		ttl:     120,
		secret:  strings.TrimSpace(c.TicketSecret),
	}
	if p.secret == "" {
		p.secret = strings.TrimSpace(h.Config.Security.ClientSignSalt)
	}
	if v := strings.TrimSpace(c.Method); v != "" {
		p.method = strings.ToUpper(v)
	}
	if v := strings.TrimSpace(c.UpstreamURL); v != "" {
		p.url = v
	}
	if v := strings.TrimSpace(c.UserAgent); v != "" {
		p.headers["User-Agent"] = v
	}
	for k, v := range c.Query {
		if k = strings.TrimSpace(k); k != "" {
			p.query[k] = v
		}
	}
	for k, v := range c.Headers {
		if k = strings.TrimSpace(k); k != "" {
			p.headers[k] = v
		}
	}
	// 可选的上游密钥片段：只从 config 读，只出现在下发的指令里（不写日志、不回显给客户端以外的任何地方）。
	if key := strings.TrimSpace(c.UpstreamKey); key != "" {
		for k, v := range p.query {
			p.query[k] = strings.ReplaceAll(v, phUpKey, key)
		}
		for k, v := range p.headers {
			p.headers[k] = strings.ReplaceAll(v, phUpKey, key)
		}
	}
	if c.TimeoutSeconds > 0 {
		p.timeout = clampInt(c.TimeoutSeconds, 1, 60)
	}
	if len(c.SuccessCodes) > 0 {
		p.success = c.SuccessCodes
	}
	if c.TicketTTLSeconds > 0 {
		p.ttl = clampInt(c.TicketTTLSeconds, 10, 600)
	}
	return p
}

// loginRecordsQueryString 按参数名 ASCII 升序拼出 query 串（顺序固定，便于客户端实现与回归）。
func loginRecordsQueryString(q map[string]string) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+q[k])
	}
	return strings.Join(parts, "&")
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ticketSign 通用票据签名（payload 为任意可 JSON 序列化的载荷）。
// 签名格式：base64url(json(payload)) + "." + hex(HMAC_SHA256(secret, base64url(payload)))。
// 抽出来是为了让「贴吧状态查询」等同类接口复用同一套票据实现，避免各写一份签名代码走偏。
func ticketSign(secret string, payload any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return body + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

// ticketVerifyRaw 通用票据验签（常数时间比较），返回票据里的原始 JSON 载荷。
func ticketVerifyRaw(secret, ticket string) ([]byte, error) {
	parts := strings.Split(strings.TrimSpace(ticket), ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errors.New("票据格式错误")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	if !hmac.Equal([]byte(hex.EncodeToString(mac.Sum(nil))), []byte(parts[1])) {
		return nil, errors.New("票据签名不匹配")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("票据载荷损坏")
	}
	return raw, nil
}

// loginRecordsSign 签名票据：base64url(payload) + "." + hex(hmacSHA256(secret, base64url(payload))).
func loginRecordsSign(secret string, t loginRecordsTicket) (string, error) {
	return ticketSign(secret, t)
}

// loginRecordsVerify 校验票据签名（常数时间比较）并解析载荷。
func loginRecordsVerify(secret, ticket string) (*loginRecordsTicket, error) {
	raw, err := ticketVerifyRaw(secret, ticket)
	if err != nil {
		return nil, err
	}
	var t loginRecordsTicket
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, errors.New("票据载荷损坏")
	}
	return &t, nil
}

// loginRecordsNonce 生成一次性 nonce（16 字节随机 hex）。
func loginRecordsNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// loginRecordsSha256 指纹（只用于票据里的 ck 绑定，不可逆）。
func loginRecordsSha256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// loginRecordsClaimPID 取本次调用应该计费的项目：优先 token 里的 pid（与 figure.go 一致），
// 老 token 没有 pid 时兜底 100006。
func loginRecordsClaimPID(claims *util.Claims) string {
	if pid := strings.TrimSpace(claims.ProjectID); pid != "" {
		return pid
	}
	return loginRecordsProjectID
}

// loginRecordsMachineCodeOf 取该用户在该项目已绑定的设备码（票据里冗余一份，便于事后核对；
// 客户端无法伪造：值来自服务端 user_binding，不取客户端入参）。
func loginRecordsMachineCodeOf(userID uint64, projectID string) string {
	var b model.UserBinding
	if err := database.DB.Select("machine_code").
		Where("user_id = ? AND project_id = ?", userID, projectID).First(&b).Error; err != nil {
		return ""
	}
	return b.MachineCode
}

// loginRecordsCheckQuota 额度预检（与 figure.go 同口径）。
// 返回 (mode, unit, 拒绝码, 拒绝文案)；拒绝码为 0 表示通过。
func loginRecordsCheckQuota(userID uint64, projectID string) (string, int, int, string) {
	ent := getEntitlement(userID, projectID)
	mode := projectMode(projectID, ent)
	unit := projectUnit(projectID)
	if mode == model.BillingMembership {
		if ent == nil || !ent.IsValid(timeNow()) {
			return mode, unit, util.CodeNoPermission, "无该项目权限"
		}
		return mode, unit, 0, ""
	}
	if ent == nil || !ent.CanConsume(mode, unit, timeNow()) {
		return mode, unit, util.CodeQuotaExhausted, modeLabel(mode) + "额度不足"
	}
	return mode, unit, 0, ""
}

// LoginRecordsPrepareRequest 申请票据入参：只需被查询账号的 ck。
type LoginRecordsPrepareRequest struct {
	Ck     string `json:"ck"`
	Cookie string `json:"cookie"` // 兼容写法
	Bduss  string `json:"bduss"`  // 兼容写法
}

// XhlLoginRecordsPrepare 用户端：申请一张「登录记录查询」授权票据（服务端不发起上游请求）。
// 计费：**成功才扣**——真正扣费发生在 /report（ok=true 且上游 code 命中 success_codes）。
//
//	POST /api/xhl/login-records/prepare      header: Authorization: Bearer <用户 token>
//	{ "ck": "BDUSS=…; STOKEN=…" }
func (h *Handler) XhlLoginRecordsPrepare(c *gin.Context) {
	claims := middleware.GetClaims(c)
	if claims == nil {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		return
	}
	pid := loginRecordsClaimPID(claims)
	started := time.Now()

	failLog := func(msg string) {
		h.logCall(c, CallLogEntry{
			ProjectID: pid, Action: model.CallActionLoginRecords, Source: model.CallSourceUser,
			UserID: claims.UserID, Username: claims.Username, Ok: false, Message: msg, Started: started,
		})
	}

	var req LoginRecordsPrepareRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failLog("参数错误：请用 JSON 提交 ck")
		util.Fail(c, util.CodeParamError, "参数错误：请用 JSON 提交（字段 ck）")
		return
	}
	ck := strings.TrimSpace(req.Ck)
	if ck == "" {
		ck = strings.TrimSpace(req.Cookie)
	}
	if ck == "" {
		ck = strings.TrimSpace(req.Bduss)
	}
	if ck == "" {
		failLog("参数错误：ck 不能为空")
		util.Fail(c, util.CodeParamError, "参数错误：ck 不能为空（完整 cookie 串）")
		return
	}
	if len(ck) > loginRecordsMaxCookie {
		failLog("参数错误：ck 过长")
		util.Fail(c, util.CodeParamError, "参数错误：ck 过长")
		return
	}
	// 上游 loginrecord 只认 BDUSS（与账号管理器 v2 的实现一致：整串 cookie 直接透传）
	if !strings.Contains(ck, "BDUSS") {
		failLog("参数错误：ck 缺少 BDUSS")
		util.Fail(c, util.CodeParamError, "cookie 无效：缺少 BDUSS")
		return
	}

	// 额度预检：不够就直接拒（此时不扣费，也不发票据）
	mode, unit, code, msg := loginRecordsCheckQuota(claims.UserID, pid)
	if code != 0 {
		failLog("拒发票据：" + msg)
		util.Fail(c, code, msg)
		return
	}

	// 项目日调用限额（复用现有 Redis 日计数，与 qrlogin 同口径；超限 1019）
	if s, err := projsetting.Get(pid); err == nil && s != nil && s.CallLimit > 0 {
		if n := h.recordDailyCall(pid, claims.UserID, model.CallActionLoginRecords); n > int64(s.CallLimit) {
			failLog("拒发票据：已达到每日使用上限")
			util.Fail(c, util.CodeCallLimitExceed, "已达到每日使用上限")
			return
		}
	}

	plan := h.loginRecordsPlanOf()
	if plan.secret == "" {
		failLog("系统错误：票据签名密钥未配置")
		util.Fail(c, util.CodeDBError, "系统错误：票据签名密钥未配置")
		return
	}
	nonce, err := loginRecordsNonce()
	if err != nil {
		failLog("系统错误：生成 nonce 失败")
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	now := timeNow()
	ticket := loginRecordsTicket{
		Ver:         loginRecordsTicketVer,
		Nonce:       nonce,
		UserID:      claims.UserID,
		Username:    claims.Username,
		ProjectID:   pid,
		MachineCode: loginRecordsMachineCodeOf(claims.UserID, pid),
		CkHash:      loginRecordsSha256(ck),
		Action:      model.CallActionLoginRecords,
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(time.Duration(plan.ttl) * time.Second).Unix(),
	}
	signed, err := loginRecordsSign(plan.secret, ticket)
	if err != nil {
		failLog("系统错误：签发票据失败")
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	h.logCall(c, CallLogEntry{
		ProjectID: pid, Action: model.CallActionLoginRecords, Source: model.CallSourceUser,
		UserID: claims.UserID, Username: claims.Username, Ok: true, Message: "已下发授权票据",
		Started: started,
	})

	util.OK(c, gin.H{
		"ticket":     signed,
		"nonce":      ticket.Nonce,
		"expires_at": ticket.ExpiresAt,
		"expires_in": plan.ttl,
		"project_id": pid,
		"billing": gin.H{
			"mode":       mode,
			"kind":       billingKindOf(mode),
			"unit":       unit,
			"charged_on": "report",
			"note":       "成功才扣费：/report 里 ok=true 且 upstream_code 命中 parse.success_codes 才扣；失败只记 call_log",
		},
		"request": gin.H{
			"method": plan.method,
			"url":    plan.url,
			"query":  plan.query,
			// query_string 顺序固定（参数名 ASCII 升序），客户端照拼即可
			"query_string": loginRecordsQueryString(plan.query),
			"headers":      plan.headers,
			"placeholders": gin.H{
				phCK:      "调用方这次提交的 ck 原串（整串 cookie），装进 Cookie 头",
				phTSMilli: "发起请求那一刻的毫秒时间戳（字符串）",
			},
			"timeout_seconds": plan.timeout,
			"note":            "由客户端用本机 IP 直连上游；服务端不代发请求，也不产生上游流量",
		},
		"parse": gin.H{
			"format":           "json",
			"code_field":       "code",
			"success_codes":    plan.success,
			"records_path":     "data.list",
			"name_field":       "tplName",
			"login_type_field": "login_type",
			"device_field":     "device",
			"name_fallback":    "name_field 为空串时，退化为「未知应用(login_type)」；再空则「未知应用(device)」；都空则「未知应用」",
			"error_codes":      loginRecordsErrorCodes,
		},
		"report_url": "/api/xhl/login-records/report",
	})
}

// loginRecordsCode 兼容上游 code 是数字或数字字符串；缺省/无法识别视为「没给」。
type loginRecordsCode struct {
	Set bool
	Val int
}

// UnmarshalJSON 实现：数字、数字字符串、null 都能吃。
func (c *loginRecordsCode) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil // 识别不了的当没给：不能凭它扣费
	}
	c.Set, c.Val = true, v
	return nil
}

// LoginRecordsReportRequest 回报入参：票据 + 客户端在本机执行的结果。
type LoginRecordsReportRequest struct {
	Ticket       string           `json:"ticket" binding:"required"`
	Ok           bool             `json:"ok"`            // 客户端是否成功发出请求并解出响应
	UpstreamCode loginRecordsCode `json:"upstream_code"` // 上游 code（扣费判定用它，防止谎报成功）
	RecordCount  int              `json:"record_count"`  // 解出的记录条数（仅留痕）
	Error        string           `json:"error"`         // 失败摘要（网络错误/解析错误等）
	DurationMS   int64            `json:"duration_ms"`
}

// XhlLoginRecordsReport 用户端：回报「登录记录查询」执行结果；成功才扣费。
//
// 校验顺序：票据签名 → 有效期 → 归属（用户/项目/动作）→ 额度 → 一次性 nonce。
// 全部通过且判定为成功才扣额度 + 写 billing_log；任何情况都写一条 call_log。
//
//	POST /api/xhl/login-records/report       header: Authorization: Bearer <用户 token>
//	{ "ticket": "…", "ok": true, "upstream_code": 110000, "record_count": 3 }
func (h *Handler) XhlLoginRecordsReport(c *gin.Context) {
	claims := middleware.GetClaims(c)
	if claims == nil {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		return
	}
	pid := loginRecordsClaimPID(claims)
	started := time.Now()

	failLog := func(code int, msg string) {
		h.logCall(c, CallLogEntry{
			ProjectID: pid, Action: model.CallActionLoginRecords, Source: model.CallSourceUser,
			UserID: claims.UserID, Username: claims.Username, Ok: false, Message: msg, Started: started,
		})
		util.Fail(c, code, msg)
	}

	var req LoginRecordsReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failLog(util.CodeParamError, "参数错误：ticket 不能为空")
		return
	}

	plan := h.loginRecordsPlanOf()
	if plan.secret == "" {
		failLog(util.CodeDBError, "系统错误：票据签名密钥未配置")
		return
	}
	t, err := loginRecordsVerify(plan.secret, req.Ticket)
	if err != nil {
		failLog(util.CodeTicketInvalid, "票据无效："+err.Error())
		return
	}
	if t.Ver != loginRecordsTicketVer {
		failLog(util.CodeTicketInvalid, "票据版本不支持")
		return
	}
	if t.Action != model.CallActionLoginRecords {
		failLog(util.CodeTicketInvalid, "票据用途不匹配")
		return
	}
	if now := timeNow().Unix(); now > t.ExpiresAt {
		failLog(util.CodeTicketInvalid, "票据已过期，请重新申请")
		return
	}
	if now := timeNow().Unix(); t.IssuedAt > now+60 {
		failLog(util.CodeTicketInvalid, "票据签发时间异常")
		return
	}
	if t.UserID != claims.UserID || t.ProjectID != pid {
		failLog(util.CodeTicketInvalid, "票据与当前登录用户/项目不匹配")
		return
	}

	// 成功判定：客户端说成功 **且** 上游 code 命中配置的成功码。
	// （只用客户端 ok 会被「什么都不做就报成功」白花钱，只用 code 又无法区分网络失败）
	success := req.Ok && req.UpstreamCode.Set && intInSlice(req.UpstreamCode.Val, plan.success)

	mode, unit, code, msg := loginRecordsCheckQuota(claims.UserID, pid)
	if code != 0 {
		failLog(code, msg)
		return
	}

	// 一次性 nonce：原子占位，重复上报（重放）直接拒
	usedKey := loginRecordsNoncePrefix + t.Nonce
	ttl := time.Duration(plan.ttl+60) * time.Second
	fresh, err := h.Redis.SetNX(context.Background(), usedKey, strconv.FormatUint(claims.UserID, 10), ttl).Result()
	if err != nil {
		failLog(util.CodeDBError, "系统错误：票据状态校验失败")
		return
	}
	if !fresh {
		failLog(util.CodeTicketInvalid, "票据已被使用（重复上报）")
		return
	}

	balanceAfter := 0
	if success {
		balanceAfter, _ = consumeQuota(claims.UserID, pid, mode, unit)
		if bk := billingKindOf(mode); bk != "" {
			h.logBilling(BillingEntry{
				ProjectID: pid, UserID: claims.UserID, Username: claims.Username,
				Kind: bk, Delta: -unit, BalanceAfter: balanceAfter,
				Reason: model.BillReasonCall, Remark: "登录记录查询成功",
			})
		}
	}

	errno := ""
	if req.UpstreamCode.Set {
		errno = strconv.Itoa(req.UpstreamCode.Val)
	}
	callMsg := ""
	if success {
		callMsg = "查询成功：" + strconv.Itoa(req.RecordCount) + " 条登录记录"
	} else if req.Error != "" {
		callMsg = "查询失败：" + req.Error
	} else if reason := loginRecordsErrorCodes[errno]; reason != "" {
		callMsg = "查询失败：" + reason
	} else if req.UpstreamCode.Set {
		callMsg = "查询失败：上游 code=" + errno
	} else {
		callMsg = "查询失败：未返回上游 code"
	}
	h.logCall(c, CallLogEntry{
		ProjectID: pid, Action: model.CallActionLoginRecords, Source: model.CallSourceUser,
		UserID: claims.UserID, Username: claims.Username,
		Ok: success, Errno: errno, Message: callMsg,
		CostKind: func() string {
			if success {
				return billingKindOf(mode)
			}
			return ""
		}(),
		Cost: func() int {
			if success && billingKindOf(mode) != "" {
				return unit
			}
			return 0
		}(),
		Started: started,
	})

	billed := success && billingKindOf(mode) != ""
	cost := 0
	if billed {
		cost = unit
	}
	util.OK(c, gin.H{
		"ok":            success,
		"billed":        billed,
		"billing_mode":  mode,
		"cost":          cost,
		"remaining":     balanceAfter,
		"upstream_code": errno,
		"record_count":  req.RecordCount,
	})
}

// intInSlice 判断 v 是否在列表里。
func intInSlice(v int, list []int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
