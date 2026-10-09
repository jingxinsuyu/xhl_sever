package model

import "time"

// 计费模式（项目级默认，用户级可覆盖）
const (
	BillingMembership = "membership" // 会员到期前可用（= v1 行为）
	BillingPerCall    = "per_call"   // 按次：每次消耗剩余次数
	BillingCredits    = "credits"    // 按积分：每次扣 Price 积分
)

// 代理模式（项目级）
const (
	ProxyInherit = "inherit" // 继承全局默认（= v1 行为，仍是全局那一个）
	ProxyNone    = "none"    // 该项目不走代理
	ProxyFixed   = "fixed"   // 固定代理地址（host:port 或 http://user:pass@host:port）
	ProxyPool    = "pool"    // 提取池（提取接口地址）
)

// ProjectSetting 项目配置（与 project 1:1；新建项目时自动建行）。
//
// 设计原则：**只加新表，不动 project 表**，因此 v1 的所有查询与接口行为零变化；
// 三个 limit 与 project 上的同名字段保持双写同步（读取以本表为准，兼容期回退到 project）。
type ProjectSetting struct {
	ProjectID string `gorm:"primaryKey;size:6" json:"project_id"`

	// ---- 计费 ----
	BillingMode string `gorm:"size:16;not null;default:'membership'" json:"billing_mode"`
	Price       int    `gorm:"not null;default:0" json:"price"` // per_call: 每次消耗次数(通常 1)；credits: 每次扣多少积分

	// ---- 限流（与 project 三个 limit 双写）----
	LoginLimit  int `gorm:"not null;default:0" json:"login_limit"`  // 0 不限；N 该用户今日登录 N 次
	UnbindLimit int `gorm:"not null;default:0" json:"unbind_limit"` // 0 不限；N 每日自助解绑 N 次
	CallLimit   int `gorm:"not null;default:0" json:"call_limit"`   // 0 不限；N 每日调用 N 次

	// ---- 代理 ----
	ProxyMode string `gorm:"size:16;not null;default:'inherit'" json:"proxy_mode"`
	ProxyURL  string `gorm:"size:255;not null;default:''" json:"proxy_url"`

	// ---- 功能开关（JSON 字符串，便于扩展而不改表）----
	Features string `gorm:"type:text" json:"features"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ProjectSetting) TableName() string { return "project_setting" }

// BillingModeOrDefault 返回归一化的计费模式（非法值按 membership 处理）。
func (s *ProjectSetting) BillingModeOrDefault() string {
	switch s.BillingMode {
	case BillingPerCall:
		return BillingPerCall
	case BillingCredits:
		return BillingCredits
	default:
		return BillingMembership
	}
}

// ProxyModeOrDefault 返回归一化的代理模式（非法值按 inherit 处理）。
func (s *ProjectSetting) ProxyModeOrDefault() string {
	switch s.ProxyMode {
	case ProxyNone, ProxyFixed, ProxyPool:
		return s.ProxyMode
	default:
		return ProxyInherit
	}
}
