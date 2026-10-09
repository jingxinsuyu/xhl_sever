package model

import "time"

// UserEntitlement 用户在某项目下的权限/剩余时间（卡密充值天数累加）
type UserEntitlement struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint64    `gorm:"not null;index:idx_user_project,unique" json:"user_id"`
	ProjectID string    `gorm:"not null;size:6;index:idx_user_project,unique" json:"project_id"`
	ExpiresAt time.Time `json:"expires_at"` // 到期时间（membership 模式用；v1 数据原样保留、值不变）
	// ---- v2 新增列（都有默认值，老数据行为与升级前完全一致）----
	BillingMode    string     `gorm:"size:16;not null;default:''" json:"billing_mode"` // '' = 跟随项目默认；否则覆盖
	Credits        int        `gorm:"not null;default:0" json:"credits"`               // 剩余积分（credits 模式）
	RemainingCalls int        `gorm:"not null;default:0" json:"remaining_calls"`       // 剩余次数（per_call 模式）
	TotalCalls     int64      `gorm:"not null;default:0" json:"total_calls"`           // 累计计费调用次数
	ExternalUID    string     `gorm:"size:64;not null;default:''" json:"external_uid"` // 项目方自己的用户号（对账/检索）
	Remark         string     `gorm:"size:255;not null;default:''" json:"remark"`
	LastCardID     uint64     `gorm:"not null;default:0" json:"last_card_id"` // 最近一次充值所用卡密 id
	LastRechargeAt *time.Time `json:"last_recharge_at"`                       // 最近一次充值时间
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (UserEntitlement) TableName() string { return "user_entitlement" }

// Mode 返回该用户在本项目上生效的计费模式：用户覆盖优先，否则用项目默认。
func (e *UserEntitlement) Mode(projectDefault string) string {
	if e.BillingMode != "" {
		return e.BillingMode
	}
	if projectDefault == "" {
		return BillingMembership
	}
	return projectDefault
}

// CanConsume 判断在该模式下能否消费 units 个单位（membership 只看有效期）。
func (e *UserEntitlement) CanConsume(mode string, units int, now time.Time) bool {
	switch mode {
	case BillingPerCall:
		return e.RemainingCalls >= units
	case BillingCredits:
		return e.Credits >= units
	default:
		return e.IsValid(now)
	}
}

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
