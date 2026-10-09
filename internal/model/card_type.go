package model

import "time"

// 卡密可发放的内容（v2 新增；days = v1 行为）
const (
	CardKindDays    = "days"    // 充值天数（会员到期时间）
	CardKindCalls   = "calls"   // 充值次数（按次计费项目）
	CardKindCredits = "credits" // 充值积分（按积分计费项目）
)

// CardType 卡密类型表（软删除，防止卡密生成使用后被删掉或报错）
type CardType struct {
	ID        uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	ProjectID string `gorm:"not null;size:6;index" json:"project_id"`
	Name      string `gorm:"size:64;not null" json:"name"`
	Days      int    `gorm:"not null;default:0" json:"days"` // 充值天数（kind=days 时与 Amount 同值，兼容老数据）
	// ---- v2 新增：卡密可发「天数 / 次数 / 积分」----
	Kind      string     `gorm:"size:16;not null;default:'days'" json:"kind"` // days | calls | credits
	Amount    int        `gorm:"not null;default:0" json:"amount"`            // 对应 kind 的数量
	DeletedAt *time.Time `gorm:"index" json:"deleted_at"`                     // 软删除时间，NULL 未删除
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (CardType) TableName() string { return "card_type" }

// KindAmount 返回归一化后的「发放类型 + 数量」，并兼容 v1 老数据：
// 老行只有 days 列、kind 为空、amount 为 0，这里一律解释成 days/days。
func (t *CardType) KindAmount() (string, int) {
	kind := t.Kind
	if kind != CardKindCalls && kind != CardKindCredits {
		kind = CardKindDays
	}
	amount := t.Amount
	if kind == CardKindDays && t.Days > 0 {
		amount = t.Days // 老数据 / 后台只填了天数
	}
	if amount <= 0 && t.Days > 0 {
		amount = t.Days
	}
	if amount < 0 {
		amount = 0
	}
	return kind, amount
}
