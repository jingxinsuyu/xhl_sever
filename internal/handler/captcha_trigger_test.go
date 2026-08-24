package handler

import (
	"context"
	"strconv"
	"testing"

	"xhl-server/internal/config"

	"github.com/redis/go-redis/v9"
)

func testConfigWithStep(step int) *config.Config {
	return &config.Config{
		Security: config.Security{
			CaptchaStep: map[string]int{"qrlogin": step},
		},
	}
}

// TestCaptchaRequired 验证码触发逻辑：
// 未达步进放行；达步进要求验证；验证后本区间放行；下区间再要求；取消后仍要求（防绕过）。
func TestCaptchaRequired(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "192.168.1.14:6379", Password: "320326"})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		t.Skip("本地 Redis 不可用:", err)
	}
	uid := uint64(99988)
	ctx := context.Background()
	passKey := captchaPassKey(uid)
	rdb.Del(ctx, passKey)

	// 构造 handler：captcha_step 用 qrlogin=20
	h := &Handler{Config: testConfigWithStep(20), Redis: rdb}

	// 第 19 次 → 放行
	if h.captchaRequired("qrlogin", uid, 19) {
		t.Fatal("第 19 次不应要求验证码")
	}
	// 第 20 次 → 要求（无凭证）
	if !h.captchaRequired("qrlogin", uid, 20) {
		t.Fatal("第 20 次应要求验证码")
	}
	// 模拟取消：第 21 次仍要求（防绕过）
	if !h.captchaRequired("qrlogin", uid, 21) {
		t.Fatal("取消验证码后第 21 次仍应要求（防绕过）")
	}
	// 模拟验证通过：存区间号 = 21/20 = 1
	rdb.Set(ctx, passKey, strconv.FormatInt(21/int64(20), 10), captchaPassTTL)
	// 第 25 次（同区间）→ 放行
	if h.captchaRequired("qrlogin", uid, 25) {
		t.Fatal("验证后同区间（21-39）应放行")
	}
	// 第 40 次（下区间 40/20=2 > 1）→ 再要求
	if !h.captchaRequired("qrlogin", uid, 40) {
		t.Fatal("第 40 次应重新要求验证码")
	}
	rdb.Del(ctx, passKey)
}
