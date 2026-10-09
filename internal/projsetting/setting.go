// Package projsetting 提供「项目配置」（计费 / 代理 / 功能开关）的读写。
//
// 兼容设计：
//   - 配置行不存在时，按 project 表现值**惰性创建**（幂等）——所以升级不需要停机迁移，
//     第一次读到某个项目时自动补一行，行为与升级前完全一致（计费=会员到期、代理=继承全局）。
//   - 三个 limit 写入时**双写回 project 表**，保证所有老代码/老接口读到的值同步。
package projsetting

import (
	"errors"
	"strings"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
)

// Get 读取项目配置；不存在则用 project 现值创建默认行（惰性迁移，幂等）。
func Get(projectID string) (*model.ProjectSetting, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("项目 id 为空")
	}

	var s model.ProjectSetting
	// 用 Find（而非 First）：无记录时不产生 GORM 的 ErrRecordNotFound 噪音日志
	if err := database.DB.Where("project_id = ?", projectID).Limit(1).Find(&s).Error; err != nil {
		return nil, err
	}
	if s.ProjectID != "" {
		return &s, nil
	}

	// 惰性创建：三个 limit 从 project 复制（= 升级前行为），其余取默认
	var p model.Project
	if err := database.DB.Where("id = ?", projectID).Limit(1).Find(&p).Error; err != nil {
		return nil, err
	}
	if p.ID == "" {
		return nil, errors.New("项目不存在")
	}
	s = model.ProjectSetting{
		ProjectID:   p.ID,
		BillingMode: model.BillingMembership,
		Price:       0,
		LoginLimit:  p.LoginLimit,
		UnbindLimit: p.UnbindLimit,
		CallLimit:   p.CallLimit,
		ProxyMode:   model.ProxyInherit,
	}
	if err := database.DB.Create(&s).Error; err != nil {
		// 并发下可能已被创建，重读一次
		if e2 := database.DB.Where("project_id = ?", projectID).Limit(1).Find(&s).Error; e2 == nil && s.ProjectID != "" {
			return &s, nil
		}
		return nil, err
	}
	return &s, nil
}

// Update 保存项目配置，并把三个 limit 双写回 project。
func Update(projectID string, in *model.ProjectSetting) (*model.ProjectSetting, error) {
	cur, err := Get(projectID)
	if err != nil {
		return nil, err
	}
	cur.BillingMode = in.BillingModeOrDefault()
	cur.Price = max0(in.Price)
	cur.LoginLimit = max0(in.LoginLimit)
	cur.UnbindLimit = max0(in.UnbindLimit)
	cur.CallLimit = max0(in.CallLimit)
	cur.ProxyMode = in.ProxyModeOrDefault()
	cur.ProxyURL = strings.TrimSpace(in.ProxyURL)
	cur.Features = strings.TrimSpace(in.Features)

	if err := database.DB.Model(&model.ProjectSetting{}).
		Where("project_id = ?", projectID).
		Updates(map[string]any{
			"billing_mode": cur.BillingMode,
			"price":        cur.Price,
			"login_limit":  cur.LoginLimit,
			"unbind_limit": cur.UnbindLimit,
			"call_limit":   cur.CallLimit,
			"proxy_mode":   cur.ProxyMode,
			"proxy_url":    cur.ProxyURL,
			"features":     cur.Features,
		}).Error; err != nil {
		return nil, err
	}

	// 双写 project：老接口（登录/init/qrlogin 读 project.*_limit）保持一致
	_ = database.DB.Model(&model.Project{}).
		Where("id = ?", projectID).
		Updates(map[string]any{
			"login_limit":  cur.LoginLimit,
			"unbind_limit": cur.UnbindLimit,
			"call_limit":   cur.CallLimit,
		}).Error

	return cur, nil
}

// EnsureAll 为所有未删除项目补齐配置行（-migrate-v2 / 后台"修复"用）。
// 返回 (新建数, 项目总数)。
func EnsureAll() (created, total int, err error) {
	var projects []model.Project
	if err = database.DB.Where("deleted_at IS NULL").Find(&projects).Error; err != nil {
		return 0, 0, err
	}
	for _, p := range projects {
		total++
		var s model.ProjectSetting
		if e := database.DB.Where("project_id = ?", p.ID).Limit(1).Find(&s).Error; e != nil {
			continue
		}
		if s.ProjectID != "" {
			continue
		}
		if _, e := Get(p.ID); e == nil {
			created++
		}
	}
	return created, total, nil
}

// BillingMode 返回项目生效的计费模式（找不到配置时回退 membership，绝不报错）。
func BillingMode(projectID string) string {
	s, err := Get(projectID)
	if err != nil || s == nil {
		return model.BillingMembership
	}
	return s.BillingModeOrDefault()
}

// Price 返回项目单价（per_call 的每次次数 / credits 的每次积分）。
func Price(projectID string) int {
	s, err := Get(projectID)
	if err != nil || s == nil {
		return 0
	}
	return s.Price
}

func max0(v int) int {
	if v < 0 {
		return 0
	}
	return v
}
