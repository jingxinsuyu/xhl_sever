package main

import (
	"log"

	"xhl-server/internal/config"
	"xhl-server/internal/database"
	"xhl-server/internal/redisclient"
	"xhl-server/internal/router"

	"github.com/gin-gonic/gin"
)

func main() {
	// 加载配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 设置 gin 模式
	gin.SetMode(cfg.Server.Mode)

	// 确保数据库存在并初始化连接
	if err := database.EnsureDatabase(&cfg.Database); err != nil {
		log.Fatalf("创建数据库失败: %v", err)
	}
	if err := database.Init(&cfg.Database); err != nil {
		log.Fatalf("初始化数据库失败: %v", err)
	}

	// 初始化 Redis（验证码、登录限流、每日登录次数存储）
	rdb, err := redisclient.Init(cfg.Redis)
	if err != nil {
		log.Fatalf("初始化 Redis 失败: %v", err)
	}

	// 启动服务
	r := router.New(cfg, rdb)
	log.Printf("xhl 后端服务已启动，监听 :%s", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
