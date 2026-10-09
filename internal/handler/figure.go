package handler

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"xhl-server/internal/database"
	"xhl-server/internal/middleware"
	"xhl-server/internal/model"
	"xhl-server/internal/tieba"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// figureProjectID 「贴吧设置虚拟形象」项目 id（用户端按项目计费；开放平台按 key 余额扣）。
const figureProjectID = "100005"

// 图片限制：类型只收 png/jpg（按文件头判断，不信扩展名与客户端的 Content-Type），单张 ≤5MB。
const (
	figureMaxImageBytes = 5 << 20 // 5MB
	figureMaxTotalBytes = 6 << 20 // 整个 multipart 请求上限（留点余量给 ck 等字段）
	figureMultipartMem  = 8 << 20
	figureMaxCookieLen  = 4096
)

// sniffImageExt 按文件头判断图片类型，返回 "png"/"jpg" 或空串（不支持）。
func sniffImageExt(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}):
		return "png"
	case len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF:
		return "jpg"
	default:
		return ""
	}
}

// newTiebaClient 造一个贴吧客户端：地址取配置（留空=官方地址），出口走该项目的代理。
func (h *Handler) newTiebaClient(ck, proxyAddr string) *tieba.Client {
	cl := tieba.NewClient(ck, proxyAddr)
	if v := strings.TrimSpace(h.Config.Tieba.UploadURL); v != "" {
		cl.UploadURL = v
	}
	if v := strings.TrimSpace(h.Config.Tieba.MetaURL); v != "" {
		cl.MetaURL = v
	}
	if v := strings.TrimSpace(h.Config.Tieba.SubmitURL); v != "" {
		cl.SubmitURL = v
	}
	return cl
}

// figureImageFromForm 从 multipart 里取 ck 与图片，并做类型/大小校验。
// 返回 (ck, 图片字节, 错误信息)；错误信息为空表示通过。
func figureImageFromForm(c *gin.Context) (string, []byte, string) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, figureMaxTotalBytes)
	if err := c.Request.ParseMultipartForm(figureMultipartMem); err != nil {
		return "", nil, "参数错误：请用 form 提交（字段 ck + 图片文件 image），单次请求不超过 6MB"
	}
	ck := strings.TrimSpace(c.Request.FormValue("ck"))
	if ck == "" {
		// 兼容把 cookie 放在 ck / cookie / bduss 三种字段名
		for _, k := range []string{"cookie", "cookies", "bduss"} {
			if v := strings.TrimSpace(c.Request.FormValue(k)); v != "" {
				ck = v
				break
			}
		}
	}
	if ck == "" {
		return "", nil, "参数错误：ck 不能为空（完整 cookie 串）"
	}
	if len(ck) > figureMaxCookieLen {
		return "", nil, "参数错误：ck 过长"
	}
	// 只认 BDUSS + SToken（贴吧设置形象靠这两个）
	if ok, missing := tieba.HasCreds(ck); !ok {
		return "", nil, "ck 里缺少 " + missing + "，请传完整 cookie"
	}

	fh, err := c.FormFile("image")
	if err != nil {
		return "", nil, "参数错误：缺少图片文件（form 字段名 image）"
	}
	if fh.Size > figureMaxImageBytes {
		return "", nil, "图片太大：最大 5MB，当前 " + humanMB(fh.Size)
	}
	f, err := fh.Open()
	if err != nil {
		return "", nil, "读取图片失败"
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, figureMaxImageBytes+1))
	if err != nil {
		return "", nil, "读取图片失败"
	}
	if len(raw) == 0 {
		return "", nil, "图片为空"
	}
	if len(raw) > figureMaxImageBytes {
		return "", nil, "图片太大：最大 5MB"
	}
	if sniffImageExt(raw) == "" {
		return "", nil, "图片格式不支持：只支持 PNG / JPG"
	}
	return ck, raw, ""
}

func humanMB(n int64) string {
	mb := float64(n) / (1 << 20)
	return strings.TrimRight(strings.TrimRight(
		// 保留一位小数
		formatFloat(mb), "0"), ".") + "MB"
}

func formatFloat(f float64) string {
	// 避免引入 strconv 之外的依赖，格式化一位小数
	i := int64(f*10 + 0.5)
	return itoa(i/10) + "." + itoa(i%10)
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// XhlFigureSet 用户端：设置贴吧虚拟形象（form 提交 ck + 图片）。
// 计费：按项目生效模式扣；**设置成功才扣费**，失败不扣（与扫码"调用即扣"不同）。
//
//	POST /api/xhl/figure/set        header: Authorization: Bearer <用户 token>
//	form: ck=<完整 cookie>&image=<图片文件>
func (h *Handler) XhlFigureSet(c *gin.Context) {
	claims := middleware.GetClaims(c)
	if claims == nil {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		return
	}
	pid := strings.TrimSpace(claims.ProjectID)
	if pid == "" {
		pid = figureProjectID // 老 token 没有 pid 就按新项目算
	}

	started := time.Now()
	ck, image, bad := figureImageFromForm(c)
	if bad != "" {
		h.logCall(c, CallLogEntry{
			ProjectID: pid, Action: model.CallActionFigureSet, Source: model.CallSourceUser,
			UserID: claims.UserID, Username: claims.Username, Ok: false, Message: bad, Started: started,
		})
		util.Fail(c, util.CodeParamError, bad)
		return
	}

	// 额度预检：不够就直接拒（此时不扣费）
	ent := getEntitlement(claims.UserID, pid)
	mode := projectMode(pid, ent)
	unit := projectUnit(pid)
	if mode == model.BillingMembership {
		if ent == nil || !ent.IsValid(timeNow()) {
			util.Fail(c, util.CodeNoPermission, "无该项目权限")
			return
		}
	} else if ent == nil || !ent.CanConsume(mode, unit, timeNow()) {
		util.Fail(c, util.CodeQuotaExhausted, modeLabel(mode)+"额度不足")
		return
	}

	proxyAddr, proxyFail := h.projectProxy(pid)
	if proxyFail {
		util.Fail(c, util.CodeNoPermission, "加载网络环境失败1000")
		return
	}

	cl := h.newTiebaClient(ck, proxyAddr)
	res, err := cl.SetFigure(c.Request.Context(), image, tieba.DefaultMeta)
	if err != nil {
		msg := err.Error()
		h.logCall(c, CallLogEntry{
			ProjectID: pid, Action: model.CallActionFigureSet, Source: model.CallSourceUser,
			UserID: claims.UserID, Username: claims.Username, Ok: false, Message: msg, Started: started,
		})
		util.Fail(c, util.CodeDBError, "设置失败："+msg)
		return
	}

	// 成功才扣费（先预检过额度，这里真扣）
	balanceAfter := 0
	if bk := billingKindOf(mode); bk != "" {
		balanceAfter, _ = consumeQuota(claims.UserID, pid, mode, unit)
		h.logBilling(BillingEntry{
			ProjectID: pid, UserID: claims.UserID, Username: claims.Username,
			Kind: bk, Delta: -unit, BalanceAfter: balanceAfter,
			Reason: model.BillReasonCall, Remark: "贴吧虚拟形象设置成功",
		})
	} else {
		// 会员制：只累计调用次数
		_, _ = consumeQuota(claims.UserID, pid, mode, unit)
	}
	h.logCall(c, CallLogEntry{
		ProjectID: pid, Action: model.CallActionFigureSet, Source: model.CallSourceUser,
		UserID: claims.UserID, Username: claims.Username, Ok: true, Message: "设置成功",
		CostKind: billingKindOf(mode), Cost: func() int {
			if billingKindOf(mode) == "" {
				return 0
			}
			return unit
		}(),
		Started: started,
	})
	util.OK(c, gin.H{
		"ok":           true,
		"pic_id":       res.PicID,
		"figure_url":   res.FigureURL,
		"billing_mode": mode,
		"cost":         unit,
		"remaining":    balanceAfter,
		"hint":         "App 里退出重进 / 下拉刷新才能看到新形象",
	})
}

// OpenFigureSet 开放平台：设置贴吧虚拟形象（form 提交 ck + 图片）。
// 计费：按 key 余额扣 config.cost.open_figure_cost（默认 1 积分）；**设置成功才扣**，失败不扣。
//
//	POST /api/open/figure/set       header: xhlkey: <sk-xxx>
//	form: ck=<完整 cookie>&image=<图片文件>
func (h *Handler) OpenFigureSet(c *gin.Context) {
	ak := middleware.GetApiKey(c)
	if ak == nil {
		util.Fail(c, util.CodeUnauthorized, "xhlkey 无效")
		return
	}
	started := time.Now()
	ck, image, bad := figureImageFromForm(c)
	if bad != "" {
		h.logCall(c, CallLogEntry{
			ProjectID: ak.ProjectID, Action: model.CallActionFigureSet, Source: model.CallSourceOpen,
			ApiKeyID: ak.ID, ApiKeyName: ak.Name, Ok: false, Message: bad, Started: started,
		})
		util.Fail(c, util.CodeParamError, bad)
		return
	}

	cost := h.openFigureCost()
	if cost > 0 && ak.Balance < cost {
		util.Fail(c, util.CodeInsufficientBalance, "积分不足")
		return
	}

	proxyAddr, proxyFail := h.projectProxy(ak.ProjectID)
	if proxyFail {
		util.Fail(c, util.CodeNoPermission, "加载网络环境失败1000")
		return
	}

	cl := h.newTiebaClient(ck, proxyAddr)
	res, err := cl.SetFigure(c.Request.Context(), image, tieba.DefaultMeta)
	if err != nil {
		msg := err.Error()
		h.logCall(c, CallLogEntry{
			ProjectID: ak.ProjectID, Action: model.CallActionFigureSet, Source: model.CallSourceOpen,
			ApiKeyID: ak.ID, ApiKeyName: ak.Name, Ok: false, Message: msg, Started: started,
		})
		util.Fail(c, util.CodeDBError, "设置失败："+msg)
		return
	}

	// 成功才扣费
	if cost > 0 {
		database.DB.Model(&model.ApiKey{}).
			Where("`key` = ? AND balance >= ?", ak.Key, cost).
			Update("balance", gorm.Expr("balance - ?", cost))
		database.DB.First(ak, ak.ID)
		h.logBilling(BillingEntry{
			ProjectID: ak.ProjectID, ApiKeyID: ak.ID, Kind: model.CardKindCredits,
			Delta: -cost, BalanceAfter: ak.Balance, Reason: model.BillReasonCall,
			Remark: "贴吧虚拟形象设置成功",
		})
	}
	h.logCall(c, CallLogEntry{
		ProjectID: ak.ProjectID, Action: model.CallActionFigureSet, Source: model.CallSourceOpen,
		ApiKeyID: ak.ID, ApiKeyName: ak.Name, Ok: true, Message: "设置成功",
		CostKind: model.CardKindCredits, Cost: cost, Started: started,
	})
	util.OK(c, gin.H{
		"ok":         true,
		"pic_id":     res.PicID,
		"figure_url": res.FigureURL,
		"cost":       cost,
		"balance":    ak.Balance,
		"hint":       "App 里退出重进 / 下拉刷新才能看到新形象",
	})
}
