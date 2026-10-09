package main

import (
	"flag"
	"log"

	"xhl-server/internal/config"
	"xhl-server/internal/database"
	"xhl-server/internal/logclean"
	"xhl-server/internal/migration"
	"xhl-server/internal/projsetting"
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
	// v2 迁移：为所有项目补齐 project_setting 配置行（幂等，可重复执行）
	migrateV2 := flag.Bool("migrate-v2", false, "为所有项目补齐 v2 配置行（project_setting，幂等）")
	// 手动清一次调用记录（定期清理会自动跑，这个用于运维手动执行）
	cleanLogs := flag.Bool("clean-logs", false, "清理过期的调用记录（保留天数取配置 log.retain_days，默认 90）")
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

	// v2 迁移模式：补齐每个项目的配置行（只加表加列，不动会员与既有数据），跑完退出
	if *migrateV2 {
		created, total, err := projsetting.EnsureAll()
		if err != nil {
			log.Fatalf("v2 迁移失败: %v", err)
		}
		log.Printf("v2 迁移完成：项目 %d 个，新建配置行 %d 条（其余已存在）", total, created)
		return
	}

	// 手动清理调用记录，跑完退出（定期清理仍由 logclean.Start 负责）
	if *cleanLogs {
		n, err := logclean.CleanOnce(cfg.Log.RetainDays)
		if err != nil {
			log.Fatalf("清理调用记录失败: %v", err)
		}
		log.Printf("调用记录清理完成：删除 %d 条", n)
		return
	}

	// 初始化 Redis（验证码、登录限流、每日登录次数存储）
	rdb, err := redisclient.Init(cfg.Redis)
	if err != nil {
		log.Fatalf("初始化 Redis 失败: %v", err)
	}

	// 调用记录定期清理（只清 call_log，扣费账本永不清；默认保留 90 天）
	logclean.Start(cfg.Log.RetainDays)

	// 启动服务
	r := router.New(cfg, rdb)
	log.Printf("xhl 后端服务已启动，监听 :%s", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
