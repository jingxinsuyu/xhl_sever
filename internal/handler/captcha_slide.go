package handler

import (
	"context"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"xhl-server/internal/captcha"
	"xhl-server/internal/crypto"
	"xhl-server/internal/middleware"
	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
)

// captchaSlideTTL 验证码答案的有效期
const captchaSlideTTL = 60 * time.Second

// captchaPassTTL 验证码通过凭证（步进区间）的有效期：覆盖整天，避免区间内重复触发
const captchaPassTTL = 24 * time.Hour

// slideCaptchaStep 返回指定接口每日调用每多少次触发一次滑动验证码（config.security.captcha_step[api]，缺省/≤0 不触发）
func (h *Handler) slideCaptchaStep(api string) int {
	if h.Config.Security.CaptchaStep == nil {
		return 0
	}
	if step := h.Config.Security.CaptchaStep[api]; step > 0 {
		return step
	}
	return 0
}

// backgroundsDir 返回滑动验证码素材目录（基于软件根目录固定解析 captcha_assets/backgrounds）。
func (h *Handler) backgroundsDir() string {
	return filepath.Join(h.Config.BaseDir, "captcha_assets", "backgrounds")
}

// captchaSlideKey 验证码答案 Redis key
func captchaSlideKey(id string) string { return "captcha:slide:" + id }

// captchaPassKey 用户已通过验证码凭证 key
func captchaPassKey(userID uint64) string {
	return "captcha:pass:" + strconv.FormatUint(userID, 10)
}

// GenSlideCaptcha 生成滑动拼图验证码（用户端，需登录）。
// 返回 {captcha_id, bg_base64, piece_base64}；答案存 Redis 60s。
func (h *Handler) GenSlideCaptcha(c *gin.Context) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// 随机背景
	bg, err := captcha.RandomBackground(h.backgroundsDir(), rng)
	if err != nil || bg == nil {
		util.Fail(c, util.CodeDBError, "验证码素材加载失败")
		return
	}
	// 生成拼图
	res := captcha.Generate(bg, rng)

	// 转 base64
	bgB64, err := encodePNGBase64(res.BG)
	if err != nil {
		util.Fail(c, util.CodeDBError, "验证码生成失败")
		return
	}
	pieceB64, err := encodePNGBase64(res.Piece)
	if err != nil {
		util.Fail(c, util.CodeDBError, "验证码生成失败")
		return
	}

	// 存答案
	id, err := randHex(16)
	if err != nil {
		util.Fail(c, util.CodeDBError, "验证码生成失败")
		return
	}
	answer := strconv.Itoa(res.GapX) + " " + strconv.Itoa(res.GapY) + " " + strconv.Itoa(res.Dir)
	if err := h.Redis.Set(context.Background(), captchaSlideKey(id), answer, captchaSlideTTL).Err(); err != nil {
		util.Fail(c, util.CodeDBError, "验证码生成失败")
		return
	}

	util.OK(c, gin.H{
		"captcha_id":   id,
		"bg_base64":    bgB64,
		"piece_base64": pieceB64,
	})
}

// slideVerifyData 前端 AES 加密传输的校验数据明文结构（仅内存中，不落网）
type slideVerifyData struct {
	X     int         `json:"x"`
	Y     int         `json:"y"`
	Trace []tracePoint `json:"trace"`
}

// tracePoint 轨迹点
type tracePoint struct {
	T int `json:"t"` // 相对起点毫秒
	X int `json:"x"`
	Y int `json:"y"`
}

// VerifySlideCaptcha 校验滑动拼图验证码（用户端，需登录）。
// 入参 {captcha_id, data}，data 为 AES 加密的 {x, y, trace}；轨迹+坐标双重校验。
func (h *Handler) VerifySlideCaptcha(c *gin.Context) {
	claims := middleware.GetClaims(c)
	if claims == nil {
		util.Fail(c, util.CodeUnauthorized, "未登录")
		return
	}
	var req struct {
		CaptchaID string `json:"captcha_id" binding:"required"`
		Data      string `json:"data" binding:"required"` // AES 密文
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		util.Fail(c, util.CodeParamError, "参数错误")
		return
	}

	// 1. AES 解密（前端用 client_aes_key 加密，不传明文）
	plain, err := crypto.AesDecryptDouble64(req.Data, h.Config.Security.ClientAESKey)
	if err != nil {
		util.Fail(c, util.CodeParamError, "数据解密失败")
		return
	}
	var vd slideVerifyData
	if err := json.Unmarshal([]byte(plain), &vd); err != nil {
		util.Fail(c, util.CodeParamError, "数据格式错误")
		return
	}

	// 2. 取答案
	ans, err := h.Redis.Get(context.Background(), captchaSlideKey(req.CaptchaID)).Result()
	if err != nil {
		util.Fail(c, util.CodeParamError, "验证码已过期")
		return
	}
	parts := strings.Fields(ans)
	if len(parts) != 3 {
		util.Fail(c, util.CodeParamError, "验证码无效")
		return
	}
	gapX, _ := strconv.Atoi(parts[0])
	gapY, _ := strconv.Atoi(parts[1])
	_, _ = strconv.Atoi(parts[2]) // dir 当前不参与校验（坐标已含位置）

	// 3. 坐标校验：X 容差 ±15（滑动对齐），Y 容差 ±10（拖动条高度固定，不需精确）
	if absInt(vd.X-gapX) > 15 || absInt(vd.Y-gapY) > 10 {
		h.Redis.Del(context.Background(), captchaSlideKey(req.CaptchaID))
		util.OK(c, gin.H{"ok": false})
		return
	}

	// 4. 轨迹校验（防机器）
	if !validateTrace(vd.Trace, vd.X) {
		h.Redis.Del(context.Background(), captchaSlideKey(req.CaptchaID))
		util.OK(c, gin.H{"ok": false})
		return
	}

	// 5. 通过：删答案，写用户通过凭证（记录当前步进区间号，覆盖整天）
	h.Redis.Del(context.Background(), captchaSlideKey(req.CaptchaID))
	interval := h.currentCaptchaInterval(claims.UserID)
	h.Redis.Set(context.Background(), captchaPassKey(claims.UserID),
		strconv.FormatInt(interval, 10), captchaPassTTL)
	util.OK(c, gin.H{"ok": true})
}

// currentCaptchaInterval 用户当前所在步进区间号 = dailyCount/step（用于记录已通过到哪一区间）
func (h *Handler) currentCaptchaInterval(userID uint64) int64 {
	count := h.todayCallCount(qrLoginProjectID, userID, "qrlogin")
	step := h.slideCaptchaStep("qrlogin")
	if step <= 0 {
		return 0
	}
	return count / int64(step)
}

// validateTrace 轨迹校验：点数/时长/无瞬移/终点对齐/末尾减速。
// 放宽阈值以降低误伤真实用户（快速/不均匀拖动也能过），但仍拦机器瞬移/直接跳目标。
func validateTrace(trace []tracePoint, finalX int) bool {
	if len(trace) < 3 {
		return false
	}
	// 总时长 300ms~8s（容忍稍慢的拖动）
	dur := trace[len(trace)-1].T - trace[0].T
	if dur < 300 || dur > 8000 {
		return false
	}
	// 终点 X 接近上报 finalX（≤15）
	last := trace[len(trace)-1].X
	if absInt(last-finalX) > 15 {
		return false
	}
	// 无瞬移：相邻点位移 ≤ 60px（拦「直接跳到目标」的 100px+ 跳变，允许较快拖动）
	for i := 1; i < len(trace); i++ {
		if absInt(trace[i].X-trace[i-1].X) > 60 {
			return false
		}
	}
	// 人类特征：末尾 2 点位移 < 10（松手前减速/停留），或全程有一次位移变小的减速
	last2 := trace[len(trace)-1].X - trace[len(trace)-2].X
	if absInt(last2) < 10 {
		return true
	}
	for i := 2; i < len(trace); i++ {
		d1 := absInt(trace[i-1].X - trace[i-2].X)
		d2 := absInt(trace[i].X - trace[i-1].X)
		if d2 < d1 && d2 < 10 {
			return true
		}
	}
	return false
}

// encodePNGBase64 图片转 data:image/png;base64
func encodePNGBase64(img image.Image) (string, error) {
	buf := &bytesBuffer{}
	if err := png.Encode(buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// bytesBuffer 简单字节缓冲（避免引入额外依赖）
type bytesBuffer struct{ b []byte }

func (b *bytesBuffer) Write(p []byte) (int, error) { b.b = append(b.b, p...); return len(p), nil }
func (b *bytesBuffer) Bytes() []byte               { return b.b }

// captchaRequired 检查用户是否需要先通过验证码：N%step==0 且无通过凭证。
// api 为接口名（如 qrlogin），dailyCount 为今日该接口调用次数（已 INCR 后）。
func (h *Handler) captchaRequired(api string, userID uint64, dailyCount int64) bool {
	step := h.slideCaptchaStep(api)
	if step <= 0 {
		return false
	}
	// 未达到触发步进点 → 放行
	if dailyCount < int64(step) {
		return false
	}
	// 当前区间号 = dailyCount/step；已通过区间 >= 当前区间 → 放行（本区间验证过）
	cur := dailyCount / int64(step)
	v, err := h.Redis.Get(context.Background(), captchaPassKey(userID)).Int64()
	if err == nil && v >= cur {
		return false
	}
	return true
}

// randHex 生成 n 字节的随机十六进制字符串
func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := crand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// absInt 整数绝对值
func absInt(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
