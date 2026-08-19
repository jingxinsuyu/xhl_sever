package model

import "time"

// CkData 用户百度账号凭证（qrlogin data 解密后存储）。
// 同一用户下按账号用户名唯一：同名账号更新，新账号插入。
// data 明文格式：用户名----密码----cookie（按 ---- 分割）。
type CkData struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64    `gorm:"not null;uniqueIndex:idx_user_username" json:"user_id"` // xhl 用户 id
	Username  string    `gorm:"size:64;not null;uniqueIndex:idx_user_username" json:"username"` // 百度账号用户名
	Password  string    `gorm:"size:255;not null;default:''" json:"-"`       // 百度账号密码
	Cookie    string    `gorm:"type:text;not null" json:"-"`                  // 百度账号 cookie 串
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (CkData) TableName() string { return "ckdata" }
