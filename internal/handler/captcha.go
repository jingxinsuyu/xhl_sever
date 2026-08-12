package handler

import (
	"context"
	"strconv"
	"strings"
	"time"

	"xhl-server/internal/util"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// captchaRedisStore 基于 Redis 的图形验证码存储：答案存 captcha:{id}，TTL 自动过期
type captchaRedisStore struct {
	rdb *redis.Client
	ttl time.Duration
}

func (s *captchaRedisStore) Set(id, value string) error {
	return s.rdb.Set(context.Background(), "captcha:"+id, value, s.ttl).Err()
}

func (s *captchaRedisStore) Get(id string, clear bool) string {
	ctx := context.Background()
	val, err := s.rdb.Get(ctx, "captcha:"+id).Result()
	if err != nil {
		return ""
	}
	if clear {
		s.rdb.Del(ctx, "captcha:"+id)
	}
	return val
}

func (s *captchaRedisStore) Verify(id, answer string, clear bool) bool {
	val := strings.TrimSpace(s.Get(id, clear))
	return val != "" && strings.EqualFold(val, strings.TrimSpace(answer))
}

// GetCaptcha 获取图形验证码（无需登录）
// 返回 {captcha_id, image}，image 为 data:image/png;base64 字符串
func (h *Handler) GetCaptcha(c *gin.Context) {
	id, b64, _, err := h.Captcha.Generate()
	if err != nil {
		util.Fail(c, util.CodeDBError, "验证码生成失败")
		return
	}
	util.OK(c, gin.H{"captcha_id": id, "image": b64})
}

// verifyCaptcha 校验图形验证码（校验后即删除，防重放）
func (h *Handler) verifyCaptcha(id, code string) bool {
	return h.Captcha.Verify(id, code, true)
}

// loginLimited 每账号每分钟登录限流（防撞库）。
// 用 redis INCR 计数，首次 INCR 时设置 1 分钟 TTL；计数超过上限返回 true。
// redis 出错时放行（fail-open），不阻塞正常登录。
func (h *Handler) loginLimited(username string) bool {
	limit := h.Config.Security.LoginRateLimitPerMin
	if limit <= 0 {
		limit = 20
	}
	ctx := context.Background()
	key := "login:limit:" + username
	count, err := h.Redis.Incr(ctx, key).Result()
	if err != nil {
		return false
	}
	if count == 1 {
		h.Redis.Expire(ctx, key, time.Minute)
	}
	return count > int64(limit)
}

// recordDailyLogin 记录今日登录次数（每次成功校验后调用），返回当前计数。
// Redis INCR，TTL 至当日结束；redis 出错时返回 0（不阻断登录）。
func (h *Handler) recordDailyLogin(projectID, userID uint64) int64 {
	date := timeNow().Format("20060102")
	ctx := context.Background()
	key := "login:daily:" + strconv.FormatUint(projectID, 10) + ":" + strconv.FormatUint(userID, 10) + ":" + date
	count, err := h.Redis.Incr(ctx, key).Result()
	if err != nil {
		return 0
	}
	if count == 1 {
		endOfDay := time.Date(timeNow().Year(), timeNow().Month(), timeNow().Day(), 23, 59, 59, 0, timeNow().Location())
		h.Redis.Expire(ctx, key, time.Until(endOfDay.Add(time.Second)))
	}
	return count
}

// todayLoginCount 查询该用户今日在该项目的登录次数（无记录返回 0）。
func (h *Handler) todayLoginCount(projectID, userID uint64) int64 {
	date := timeNow().Format("20060102")
	key := "login:daily:" + strconv.FormatUint(projectID, 10) + ":" + strconv.FormatUint(userID, 10) + ":" + date
	n, err := h.Redis.Get(context.Background(), key).Int64()
	if err != nil {
		return 0
	}
	return n
}

// clearDailyLogin 清除该用户今日登录计数。projectID=0 清除该用户全部项目。
func (h *Handler) clearDailyLogin(projectID, userID uint64) error {
	ctx := context.Background()
	pattern := "login:daily:*:" + strconv.FormatUint(userID, 10) + ":*"
	if projectID > 0 {
		pattern = "login:daily:" + strconv.FormatUint(projectID, 10) + ":" + strconv.FormatUint(userID, 10) + ":*"
	}
	iter := h.Redis.Scan(ctx, 0, pattern, 100).Iterator()
	var keys []string
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
	}
	if err := iter.Err(); err != nil {
		return err
	}
	if len(keys) > 0 {
		return h.Redis.Del(ctx, keys...).Err()
	}
	return nil
}

// exceedDailyUnbindLimit 该用户今日在该项目的自助解绑次数是否已达上限（limit>0 时才调用）。
// 用 redis INCR 计数，TTL 至当日结束；redis 出错时放行。
func (h *Handler) exceedDailyUnbindLimit(limit int, projectID, userID uint64) bool {
	date := timeNow().Format("20060102")
	ctx := context.Background()
	key := "unbind:daily:" + strconv.FormatUint(projectID, 10) + ":" + strconv.FormatUint(userID, 10) + ":" + date
	count, err := h.Redis.Incr(ctx, key).Result()
	if err != nil {
		return false
	}
	if count == 1 {
		endOfDay := time.Date(timeNow().Year(), timeNow().Month(), timeNow().Day(), 23, 59, 59, 0, timeNow().Location())
		h.Redis.Expire(ctx, key, time.Until(endOfDay.Add(time.Second)))
	}
	return count > int64(limit)
}
