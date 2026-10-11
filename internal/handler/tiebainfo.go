// 「贴吧状态查询器」授权 / 计费接口（客户机本地执行 + 服务端只授权与计费）。
//
// ★ 架构（与 loginrecords.go 完全同口径，不要改成服务端代理）：**服务端不产生任何上游流量**。
//
//	上游是贴吧的公开主页接口 home/get/panel（吧龄/发帖数/发言数）；
//	服务端一台机器的出口 IP 去扫成千上万个贴吧主页几乎必然被限流/风控，
//	所以真正的 HTTP 请求必须由**客户端用本机 IP** 发出。服务端只做三件事：
//	  1. POST /api/xhl/tieba-status/prepare —— 鉴权 + 额度预检 + 下发一张
//	     「短时效 + 一次性 nonce + 绑定用户/项目/设备码」的**授权票据**（HMAC 签名）；
//	     票据里带**上游请求的拼装指令**（method / URL / query 顺序 / headers / 占位符）、
//	     **字段映射**（吧龄/发帖数/发言数 的点分路径 + 单位换算 factor）与**错误码释义**。
//	  2. POST /api/xhl/tieba-status/report —— 客户端回报执行结果：校验票据签名/有效期/归属
//	     与 nonce 未被用过；成功后写 call_log（会员制不按次扣，billing_log 仅在真需要计费时写）。
//	  3. 协议、域名、参数、字段路径、单位、错误码全部来自 config.yaml 的 tieba_status 段：
//	     上游变了只改配置重启，老客户端不用重新发版。
//
// 诚实边界（同 loginrecords.go）：客户端既然要直连上游，本机就能拿到上游数据；票据只能挡
// 「重放、跨设备盗用、无票据白嫖」，挡不住「已授权客户端谎报失败」。防线 = 一次性 nonce +
// 短时效 + 用户/项目/设备码绑定 + 项目日调用限额 + call_log 全量留痕。
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/projsetting"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// tiebaStatusProjectID 「小火龙贴吧状态查询器」项目 id（用户 token 无 pid 时的兜底）。
const tiebaStatusProjectID = "100007"

// 默认协议参数（照 小火龙账号管理器v2/baidu/accountcheck/check.go 的抓包实现）。
const (
	// tiebaStatusDefaultURL 贴吧主页公开接口（追加 &un=用户名&red_tag=10 位随机数）。
	tiebaStatusDefaultURL = "https://tieba.baidu.com/home/get/panel"
	// tiebaStatusDefaultUA 网页接口 UA（照抓包；check.go 的 WebUA）。
	tiebaStatusDefaultUA = "Mozilla/5.0 (Windows NT 10.0; WOW64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/86.0.4240.198 Safari/537.36"
	tiebaStatusMaxCookie   = 4096
	tiebaStatusTicketVer   = 1
	tiebaStatusNoncePrefix = "xhl:tieba-status:used:"
	// 占位符：由客户端在发送请求时替换（服务端不下发 ck 原串，只下发模板）。
	phTiebaUN      = "${UN}"
	phTiebaRedTag  = "${RED_TAG}"
	phTiebaTSMilli = "${TS_MS}"
)

// tiebaStatusErrorCodes 上游 no / errno 释义（随票据下发，客户端不必内置这套表）。
// 取值来自 小火龙账号管理器v2/baidu/accountcheck/check.go 的实测响应：
//   - home/get/panel：no=0 成功；no=1130032 + error="被屏蔽"（data 是空数组）
//   - pmc/listmsg / initNickname：errno / error_code = 230004「请先登录」= 未登录/登录态失效
//
// 说明：panel 的 `error` 字段本身就是百度给的中文串，客户端优先直接展示它，
// 这张表是「上游只给数字不给文案」时的兜底。
var tiebaStatusErrorCodes = map[string]string{
	"0":       "查询成功",
	"1130032": "被屏蔽（账号主页被隐私屏蔽/无主页）",
	"230004":  "未登录（登录态失效，请重新登录该账号）",
	"50000":   "触发风控",
	"1":       "账号异常（接口未返回可用的吧龄/发帖数）",
	"-1":      "账号异常（上游返回异常状态）",
}

// tiebaStatusTicket 票据载荷（整体经 HMAC-SHA256 签名，客户端不得改动）。
// 注意：绝不把 ck 原文放进票据，只放 sha256(ck) 指纹，票据被截获也无法还原 ck。
type tiebaStatusTicket struct {
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

// errTicketPayloadBroken 票据载荷损坏（与 loginrecords.go 同文案）。
var errTicketPayloadBroken = errors.New("票据载荷损坏")

// tiebaStatusPlan 归一化后的协议计划（config 覆盖内置默认）。
type tiebaStatusPlan struct {
	method  string
	url     string
	query   map[string]string
	headers map[string]string
	timeout int
	success []int
	ttl     int
	secret  string

	// 字段映射（点分路径 + 展示单位 + 换算 factor）。
	agePath     string
	postPath    string
	commentPath string
	ageFactor   float64
	ageFromUnit string
	ageToUnit   string
	errorCodes  map[string]string
	daysPerYear int
}

// tiebaStatusPlanOf 读取 config.yaml tieba_status 段并归一化（缺项一律回退内置默认）。
func (h *Handler) tiebaStatusPlanOf() tiebaStatusPlan {
	c := h.Config.TiebaStatus

	p := tiebaStatusPlan{
		method: "GET",
		url:    tiebaStatusDefaultURL,
		query: map[string]string{
			"ie":      "utf-8",
			"un":      phTiebaUN,     // 用户名（缺用户名时客户端整条 query 去掉，改用 Cookie 查自己）
			"red_tag": phTiebaRedTag, // 10 位随机数：上游不校验，只为避免固定指纹
		},
		headers: map[string]string{
			"Cookie":           phTiebaCK,
			"User-Agent":       tiebaStatusDefaultUA,
			"Accept":           "*/*",
			"Accept-Language":  "zh-CN",
			"X-Requested-With": "XMLHttpRequest",
		},
		timeout:     15,
		success:     []int{0},
		ttl:         120,
		secret:      strings.TrimSpace(c.TicketSecret),
		agePath:     "data.tb_age",
		postPath:    "data.post_num",
		commentPath: "data.followed_count",
		ageFromUnit: "year",
		ageToUnit:   "year",
		ageFactor:   1,
		daysPerYear: 365,
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
	// 可选的上游密钥片段：只从 config 读，只出现在下发的指令里（不写日志、不回显到别处）。
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
	if v := strings.TrimSpace(c.AgeField); v != "" {
		p.agePath = v
	}
	if v := strings.TrimSpace(c.PostField); v != "" {
		p.postPath = v
	}
	if v := strings.TrimSpace(c.CommentField); v != "" {
		p.commentPath = v
	}
	if c.DaysPerYear > 0 {
		p.daysPerYear = clampInt(c.DaysPerYear, 1, 1000)
	}
	switch strings.ToLower(strings.TrimSpace(c.AgeUnit)) {
	case "", "year", "years", "年":
		p.ageFromUnit, p.ageToUnit, p.ageFactor = "year", "year", 1
	case "day", "days", "天":
		// 上游给「天」：换算成「年」展示（客户端 value × factor）
		p.ageFromUnit, p.ageToUnit = "day", "year"
		p.ageFactor = 1 / float64(p.daysPerYear)
	default:
		// 不认识的单位：原样透传（factor=1），不猜
		p.ageFromUnit, p.ageToUnit, p.ageFactor = strings.ToLower(strings.TrimSpace(c.AgeUnit)), strings.ToLower(strings.TrimSpace(c.AgeUnit)), 1
	}
	p.errorCodes = make(map[string]string, len(tiebaStatusErrorCodes)+len(c.ErrorCodes))
	for k, v := range tiebaStatusErrorCodes {
		p.errorCodes[k] = v
	}
	for k, v := range c.ErrorCodes {
		if k = strings.TrimSpace(k); k != "" {
			p.errorCodes[k] = v
		}
	}
	return p
}

// phTiebaCK ${CK} 占位符（与 loginrecords.go 的 phCK 同值，单独列一个便于本文件自洽阅读）。
const phTiebaCK = phCK

// tiebaStatusVerifyTicket 校验贴吧状态票据签名并解析载荷。
func tiebaStatusVerifyTicket(secret, ticket string) (*tiebaStatusTicket, error) {
	raw, err := ticketVerifyRaw(secret, ticket)
	if err != nil {
		return nil, err
	}
	var t tiebaStatusTicket
	if err := json.Unmarshal(raw, &t); err != nil {
		return nil, errTicketPayloadBroken
	}
	return &t, nil
}

// tiebaStatusClaimPID 取本次调用应该计费的项目：优先 token 里的 pid，
// 老 token 没有 pid 时兜底 100007。
func tiebaStatusClaimPID(claims *util.Claims) string {
	if pid := strings.TrimSpace(claims.ProjectID); pid != "" {
		return pid
	}
	return tiebaStatusProjectID
}

// TiebaStatusPrepareRequest 申请票据入参：只需被查询账号的 ck。
type TiebaStatusPrepareRequest struct {
	Ck     string `json:"ck"`
	Cookie string `json:"cookie"` // 兼容写法
	Bduss  string `json:"bduss"`  // 兼容写法
	// Username 可选：导入行第 1 段的用户名（用于 panel 的 un 参数）。
	// 不下发进票据、不落库，只是让客户端少走一步；服务端不校验也不存储。
	Username string `json:"username"`
}

// XhlTiebaStatusPrepare 用户端：申请一张「贴吧状态查询」授权票据（服务端不发起上游请求）。
//
//	POST /api/xhl/tieba-status/prepare      header: Authorization: Bearer <用户 token>
//	{ "ck": "BDUSS=…; STOKEN=…", "username": "可选用户名" }
func (h *Handler) XhlTiebaStatusPrepare(c *gin.Context) {
	claims := middleware.GetClaims(c)
	if claims == nil {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		return
	}
	pid := tiebaStatusClaimPID(claims)
	started := time.Now()

	failLog := func(msg string) {
		h.logCall(c, CallLogEntry{
			ProjectID: pid, Action: model.CallActionTiebaStatus, Source: model.CallSourceUser,
			UserID: claims.UserID, Username: claims.Username, Ok: false, Message: msg, Started: started,
		})
	}

	var req TiebaStatusPrepareRequest
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
	if len(ck) > tiebaStatusMaxCookie {
		failLog("参数错误：ck 过长")
		util.Fail(c, util.CodeParamError, "参数错误：ck 过长")
		return
	}
	if !strings.Contains(strings.ToUpper(ck), "BDUSS") {
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

	// 项目日调用限额（复用现有 Redis 日计数；超限 1019）
	if s, err := projsetting.Get(pid); err == nil && s != nil && s.CallLimit > 0 {
		if n := h.recordDailyCall(pid, claims.UserID, model.CallActionTiebaStatus); n > int64(s.CallLimit) {
			failLog("拒发票据：已达到每日使用上限")
			util.Fail(c, util.CodeCallLimitExceed, "已达到每日使用上限")
			return
		}
	}

	plan := h.tiebaStatusPlanOf()
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
	ticket := tiebaStatusTicket{
		Ver:         tiebaStatusTicketVer,
		Nonce:       nonce,
		UserID:      claims.UserID,
		Username:    claims.Username,
		ProjectID:   pid,
		MachineCode: loginRecordsMachineCodeOf(claims.UserID, pid),
		CkHash:      loginRecordsSha256(ck),
		Action:      model.CallActionTiebaStatus,
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(time.Duration(plan.ttl) * time.Second).Unix(),
	}
	signed, err := ticketSign(plan.secret, ticket)
	if err != nil {
		failLog("系统错误：签发票据失败")
		util.Fail(c, util.CodeDBError, "系统错误")
		return
	}

	h.logCall(c, CallLogEntry{
		ProjectID: pid, Action: model.CallActionTiebaStatus, Source: model.CallSourceUser,
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
			"note":       "会员制：会员有效期内可查询，不按次扣费；失败只记 call_log",
		},
		"request": gin.H{
			"method": plan.method,
			"url":    plan.url,
			"query":  plan.query,
			// query_string 顺序固定（参数名 ASCII 升序），客户端照拼即可
			"query_string": loginRecordsQueryString(plan.query),
			"headers":      plan.headers,
			"placeholders": gin.H{
				phTiebaCK:      "调用方这次提交的 ck 原串（整串 cookie），装进 Cookie 头",
				phTiebaUN:      "导入行第 1 段的用户名（UTF-8 由客户端自行 URL 编码）",
				phTiebaRedTag:  "10 位随机数字（上游不校验，只为避免固定指纹）",
				phTiebaTSMilli: "发起请求那一刻的毫秒时间戳（字符串）",
			},
			"optional_params": gin.H{
				"un": "用户名缺失（纯 ck 行）时，客户端应整条去掉 un 参数并保留 Cookie 头，改为查询该 ck 自己的主页",
			},
			"timeout_seconds": plan.timeout,
			"note":            "由客户端用本机 IP 直连上游；服务端不代发请求，也不产生上游流量",
		},
		"parse": gin.H{
			"format":        "json",
			"code_field":    "no",
			"message_field": "error",
			"success_codes": plan.success,
			// data_path 为对象时取字段映射；为数组（被屏蔽）时按上游 error 文案处理
			"data_path": "data",
			// name_field 只用于客户端明细展示（相对**整个响应**的点分路径）
			"name_field": "data.name",
			"fields": gin.H{
				"age": gin.H{
					"path":      plan.agePath,
					"label":     "吧龄",
					"from_unit": plan.ageFromUnit,
					"to_unit":   plan.ageToUnit,
					"factor":    plan.ageFactor,
					"note":      "上游原始值 × factor = 展示值（单位 to_unit）；缺字段时留空",
				},
				"post": gin.H{
					"path":      plan.postPath,
					"label":     "发帖数",
					"from_unit": "count",
					"to_unit":   "count",
					"factor":    1,
					"note":      "整数计数；缺字段时兜底 0",
				},
				"comment": gin.H{
					"path":      plan.commentPath,
					"label":     "发言数",
					"from_unit": "count",
					"to_unit":   "count",
					"factor":    1,
					"note":      "整数计数（上游字段名叫 followed_count）；缺字段时兜底 0",
				},
			},
			"degraded_note": "no=0 但 age 与 post 两个字段都缺 = 上游降级响应（通常被限流），按失败处理，不要当成「查询成功但全 0」写库",
			"value_note":    "字段值可能是 JSON 数字或数字字符串；还可能带单位（如 \"3年\"、\"1.2万\"），客户端需容错解析",
			"error_codes":   plan.errorCodes,
		},
		"report_url": "/api/xhl/tieba-status/report",
	})
}

// TiebaStatusReportRequest 回报入参：票据 + 客户端在本机执行的结果。
type TiebaStatusReportRequest struct {
	Ticket       string           `json:"ticket" binding:"required"`
	Ok           bool             `json:"ok"`            // 客户端是否成功发出请求并解出响应
	UpstreamCode loginRecordsCode `json:"upstream_code"` // 上游 no（扣费判定用它，防止谎报成功）
	Status       string           `json:"status"`        // 客户端判定的中文状态（仅留痕）
	Age          string           `json:"age"`           // 吧龄（仅留痕，便于排查字段映射）
	PostNum      string           `json:"post_num"`      // 发帖数（仅留痕）
	CommentNum   string           `json:"comment_num"`   // 发言数（仅留痕）
	Error        string           `json:"error"`         // 失败摘要（网络错误/解析错误等）
	DurationMS   int64            `json:"duration_ms"`
}

// XhlTiebaStatusReport 用户端：回报「贴吧状态查询」执行结果。
//
// 校验顺序：票据签名 → 有效期 → 归属（用户/项目/动作）→ 额度 → 一次性 nonce。
// **计费：与 100006 一样按会员制，不按次扣**——所以这里不扣额度；
// billed 仅在项目实际配成按次/积分时才可能为 true（与 figure.go 同口径，正常不会发生）。
//
//	POST /api/xhl/tieba-status/report       header: Authorization: Bearer <用户 token>
//	{ "ticket": "…", "ok": true, "upstream_code": 0, "age": "8.5", "post_num": "12", "comment_num": "34" }
func (h *Handler) XhlTiebaStatusReport(c *gin.Context) {
	claims := middleware.GetClaims(c)
	if claims == nil {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		return
	}
	pid := tiebaStatusClaimPID(claims)
	started := time.Now()

	failLog := func(code int, msg string) {
		h.logCall(c, CallLogEntry{
			ProjectID: pid, Action: model.CallActionTiebaStatus, Source: model.CallSourceUser,
			UserID: claims.UserID, Username: claims.Username, Ok: false, Message: msg, Started: started,
		})
		util.Fail(c, code, msg)
	}

	var req TiebaStatusReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		failLog(util.CodeParamError, "参数错误：ticket 不能为空")
		return
	}

	plan := h.tiebaStatusPlanOf()
	if plan.secret == "" {
		failLog(util.CodeDBError, "系统错误：票据签名密钥未配置")
		return
	}
	t, err := tiebaStatusVerifyTicket(plan.secret, req.Ticket)
	if err != nil {
		failLog(util.CodeTicketInvalid, "票据无效："+err.Error())
		return
	}
	if t.Ver != tiebaStatusTicketVer {
		failLog(util.CodeTicketInvalid, "票据版本不支持")
		return
	}
	if t.Action != model.CallActionTiebaStatus {
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

	// 成功判定：客户端说成功 **且** 上游 no 命中配置的成功码。
	success := req.Ok && req.UpstreamCode.Set && intInSlice(req.UpstreamCode.Val, plan.success)

	mode, unit, code, msg := loginRecordsCheckQuota(claims.UserID, pid)
	if code != 0 {
		failLog(code, msg)
		return
	}

	// 一次性 nonce：原子占位，重复上报（重放）直接拒
	usedKey := tiebaStatusNoncePrefix + t.Nonce
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

	// 计费：本项目按会员制，**不按次扣**。
	// 只有项目被配成 per_call/credits（不是本项目设计口径）时才走额度扣减与 billing_log。
	billable := success && billingKindOf(mode) != ""
	charged := billable && mode != model.BillingMembership
	balanceAfter := 0
	if charged {
		balanceAfter, _ = consumeQuota(claims.UserID, pid, mode, unit)
		h.logBilling(BillingEntry{
			ProjectID: pid, UserID: claims.UserID, Username: claims.Username,
			Kind: billingKindOf(mode), Delta: -unit, BalanceAfter: balanceAfter,
			Reason: model.BillReasonCall, Remark: "贴吧状态查询成功",
		})
	}

	errno := ""
	if req.UpstreamCode.Set {
		errno = strconv.Itoa(req.UpstreamCode.Val)
	}
	callMsg := ""
	statusText := strings.TrimSpace(req.Status)
	switch {
	case success:
		parts := make([]string, 0, 3)
		if s := strings.TrimSpace(req.Status); s != "" {
			parts = append(parts, "状态="+s)
		}
		if s := strings.TrimSpace(req.Age); s != "" {
			parts = append(parts, "吧龄="+s)
		}
		if s := strings.TrimSpace(req.PostNum); s != "" {
			parts = append(parts, "发帖数="+s)
		}
		if s := strings.TrimSpace(req.CommentNum); s != "" {
			parts = append(parts, "发言数="+s)
		}
		callMsg = "查询成功"
		if len(parts) > 0 {
			callMsg += "：" + strings.Join(parts, " ")
		}
	case req.Error != "":
		callMsg = "查询失败：" + req.Error
	case statusText != "":
		callMsg = "查询失败：" + statusText
	case plan.errorCodes[errno] != "":
		callMsg = "查询失败：" + plan.errorCodes[errno]
	case req.UpstreamCode.Set:
		callMsg = "查询失败：上游 no=" + errno
	default:
		callMsg = "查询失败：未返回上游 no"
	}
	h.logCall(c, CallLogEntry{
		ProjectID: pid, Action: model.CallActionTiebaStatus, Source: model.CallSourceUser,
		UserID: claims.UserID, Username: claims.Username,
		Ok: success, Errno: errno, Message: callMsg,
		CostKind: func() string {
			if charged {
				return billingKindOf(mode)
			}
			return ""
		}(),
		Cost: func() int {
			if charged {
				return unit
			}
			return 0
		}(),
		Started: started,
	})

	cost := 0
	if charged {
		cost = unit
	}
	util.OK(c, gin.H{
		"ok":            success,
		"billed":        charged,
		"billing_mode":  mode,
		"cost":          cost,
		"remaining":     balanceAfter,
		"upstream_code": errno,
		"status":        req.Status,
		"age":           req.Age,
		"post_num":      req.PostNum,
		"comment_num":   req.CommentNum,
	})
}
