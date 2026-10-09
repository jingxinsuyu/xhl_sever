package redisclient

import (
	"context"
	"fmt"
	"time"

	"xhl-server/internal/config"

	"github.com/redis/go-redis/v9"
)

// Init 创建 Redis 客户端并做连通性校验
func Init(cfg config.Redis) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}
