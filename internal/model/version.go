package model

import "time"

// 版本平台
const (
	PlatformAndroid = "android" // 安卓端
	PlatformPC      = "pc"      // PC 端
	PlatformIOS     = "ios"     // iOS 端
)

// Version 项目版本表（按平台：android/pc/ios，版本号 + 更新文件）
type Version struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID string    `gorm:"not null;size:6;index" json:"project_id"`
	Platform  string    `gorm:"size:16;not null;default:''" json:"platform"` // android / pc / ios
	Version   string    `gorm:"size:64;not null" json:"version"`             // 版本号
	FileName  string    `gorm:"size:255;not null;default:''" json:"file_name"`
	FilePath  string    `gorm:"size:255;not null;default:''" json:"file_path"` // 存储相对路径
	FileSize  int64     `gorm:"not null;default:0" json:"file_size"`
	CreatedAt time.Time `json:"created_at"`
}

func (Version) TableName() string { return "version" }
