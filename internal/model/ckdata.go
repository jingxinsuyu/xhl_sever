package model

import "time"

// CkData 用户百度账号凭证（qrlogin data 解密后存储）。
// 同一用户下按账号用户名唯一：同名账号更新，新账号插入。
// data 明文格式：用户名----密码----cookie（按 ---- 分割）。
type CkData struct {
	ID     uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID uint64 `gorm:"not null;uniqueIndex:idx_user_username" json:"user_id"` // xhl 用户 id（0 = 第三方/开放平台）
	// ApiKeyID v2 新增：写入这条记录的开放平台 key id（0=老数据/用户端）。
	// 仅用于归属与对账，不参与唯一键，老数据行为不变。
	ApiKeyID uint64 `gorm:"not null;default:0;index" json:"api_key_id"`
	// ProjectID：写入这条记录时所属的项目 id。
	// 用户端取 JWT 的 pid，开放平台取 key 所属项目；仅用于后台按项目筛选，不参与业务判断。
	//
	// ⚠️ 这里**故意用 size:64 而不是 size:6**：老库里 ckdata 可能已经有一列 project_id
	// varchar(64)（历史遗留，里面装的是 16 位 hex 的旧项目 id）。
	// 如果按 size:6 声明，AutoMigrate 会执行 `ALTER TABLE ckdata MODIFY project_id varchar(6)`，
	// 一旦有旧值超过 6 个字符就会报 1406 Data too long，**整个服务直接起不来**；
	// 而且"改类型"本身就违反 v2 的红线（只加列、不改类型）。
	// 声明成 64 时：老库上类型一致 → 不做任何 ALTER；新库上建出来就是 varchar(64)，装 6 位 id 完全够。
	ProjectID string    `gorm:"size:64;not null;default:'';index" json:"project_id"`
	Username  string    `gorm:"size:64;not null;uniqueIndex:idx_user_username" json:"username"` // 百度账号用户名
	Password  string    `gorm:"size:255;not null;default:''" json:"-"`                          // 百度账号密码
	Cookie    string    `gorm:"type:text;not null" json:"-"`                                    // 百度账号 cookie 串
	Exported  bool      `gorm:"default:false;index" json:"exported"`                            // 是否已导出（后台 cookie 库导出后标记）
	Source    string    `gorm:"size:64;not null;default:''" json:"source"`                      // 来源标签：用户:用户名 / 开放平台:key名；历史数据为空
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (CkData) TableName() string { return "ckdata" }
