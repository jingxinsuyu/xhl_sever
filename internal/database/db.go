package database

import (
	"fmt"

	"xhl-server/internal/config"
	"xhl-server/internal/model"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

// Init 初始化数据库连接并自动建表
func Init(cfg *config.Database) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName, cfg.Charset)

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return fmt.Errorf("连接数据库失败: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("获取底层连接失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)

	// 自动建表
	if err := db.AutoMigrate(
		&model.Admin{},
		&model.User{},
		&model.Project{},
		&model.UserEntitlement{},
		&model.UserBinding{},
		&model.Version{},
		&model.ProjectVariable{},
		&model.Carousel{},
		&model.RichTextAd{},
		&model.CardType{},
		&model.Card{},
		&model.CkData{},
	); err != nil {
		return fmt.Errorf("自动建表失败: %w", err)
	}

	DB = db
	return nil
}

// EnsureDatabase 数据库不存在时自动创建
func EnsureDatabase(cfg *config.Database) error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/?charset=%s&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Charset)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	sql := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET %s COLLATE %s",
		cfg.DBName, cfg.Charset, cfg.Charset+"_unicode_ci")
	return db.Exec(sql).Error
}
