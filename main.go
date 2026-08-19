package main

import (
	"flag"
	"log"

	"xhl-server/internal/config"
	"xhl-server/internal/database"
	"xhl-server/internal/migration"
	"xhl-server/internal/redisclient"
	"xhl-server/internal/router"

	"github.com/gin-gonic/gin"
)

func main() {
	// 一次性迁移：将 project 自增 id 迁移为用户自定义 6 位字符串（备份后执行，跑完退出）
	migrateID := flag.Bool("migrate-project-id", false, "迁移 project id 为 6 位字符串（含快照备份）")
	projectName := flag.String("project-name", "小火龙", "识别为目标项目(100001) 的名称关键字")
	targetID := flag.String("target-id", "100001", "目标项目的新 id（6 位数字）")
	dbHost := flag.String("db-host", "", "迁移时覆盖数据库 host（宿主机直连 Docker 容器用 127.0.0.1）")
	flag.Parse()

	// 加载配置
	cfg, err := config.Load("config.yaml")
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	// 迁移模式：只跑迁移，不启动服务
	if *migrateID {
		if *dbHost != "" {
			cfg.Database.Host = *dbHost
		}
		if err := migration.RunProjectID(&cfg.Database, cfg.BaseDir, *projectName, *targetID); err != nil {
			log.Fatalf("迁移失败: %v", err)
		}
		log.Println("project id 迁移完成")
		return
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
