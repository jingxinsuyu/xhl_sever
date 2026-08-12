package model

import "time"

// UserEntitlement 用户在某项目下的权限/剩余时间（卡密充值天数累加）
type UserEntitlement struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64    `gorm:"not null;index:idx_user_project,unique" json:"user_id"`
	ProjectID uint64    `gorm:"not null;index:idx_user_project,unique" json:"project_id"`
	ExpiresAt time.Time `json:"expires_at"` // 到期时间
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (UserEntitlement) TableName() string { return "user_entitlement" }

// AddDays 累加天数，已过期则从当前时间起算
func (e *UserEntitlement) AddDays(days int, now time.Time) {
	var base time.Time
	if e.ExpiresAt.After(now) {
		base = e.ExpiresAt
	} else {
		base = now
	}
	e.ExpiresAt = base.AddDate(0, 0, days)
}

// IsValid 是否仍有效（未过期）
func (e *UserEntitlement) IsValid(now time.Time) bool {
	return e.ExpiresAt.After(now)
}
