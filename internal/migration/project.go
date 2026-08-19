// Package migration 一次性数据迁移工具（`xhl_sever -migrate-project-id`）。
// 将 project 自增 id（BIGINT）迁移为用户自定义的 6 位数字字符串：
//   - 目标项目（默认按名含「小火龙」识别，回退 id=1）→ 100001
//   - 其余项目 → LPAD(旧id, 6, '0')
// 同步迁移 8 张引用表（user_entitlement/user_binding/version/project_variable/
// carousel/rich_text_ad/card_type/card）的 project_id，并自动导出 JSON 快照供回滚。
package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"xhl-server/internal/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// refTables 引用 project_id 的表（gorm 无外键约束，需逐一迁移）。
var refTables = []string{
	"user_entitlement",
	"user_binding",
	"version",
	"project_variable",
	"carousel",
	"rich_text_ad",
	"card_type",
	"card",
}

// ProjectRow 迁移前读取的 project 行。
type ProjectRow struct {
	ID   uint64
	Name string
}

// alreadyMigrated 检测 project.id 列是否已是 varchar（已迁移过）。
func alreadyMigrated(db *gorm.DB) (bool, error) {
	var dataType string
	err := db.Raw(
		"SELECT DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'project' AND COLUMN_NAME = 'id'",
	).Scan(&dataType).Error
	if err != nil {
		return false, err
	}
	return dataType == "varchar", nil
}

// Connect 直连数据库（不跑 AutoMigrate，避免与手工迁移冲突）。
func Connect(cfg *config.Database) (*gorm.DB, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=%s&parseTime=True&loc=Local",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.DBName, cfg.Charset)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return db, nil
}

// BuildMapping 计算旧 id → 新 id 映射。
// targetName 非空时按名含关键字识别目标项目，否则回退 id=1；两者都没有则报错。
// 其余项目 → LPAD(旧id, 6, '0')。
func BuildMapping(rows []ProjectRow, targetName, targetID string) (map[uint64]string, error) {
	if len(rows) == 0 {
		return nil, errors.New("project 表为空，无需迁移")
	}
	// 目标项目
	var target *ProjectRow
	if strings.TrimSpace(targetName) != "" {
		for i := range rows {
			if strings.Contains(rows[i].Name, targetName) {
				target = &rows[i]
				break
			}
		}
	}
	if target == nil {
		for i := range rows {
			if rows[i].ID == 1 {
				target = &rows[i]
				break
			}
		}
	}
	if target == nil {
		return nil, fmt.Errorf("未找到目标项目（名含 %q 或 id=1），请用 -project-name 指定", targetName)
	}

	mapping := make(map[uint64]string, len(rows))
	seen := map[string]bool{targetID: true} // 目标项目独占 targetID
	for _, r := range rows {
		if r.ID == target.ID {
			mapping[r.ID] = targetID
			continue
		}
		newID := fmt.Sprintf("%06d", r.ID)
		if seen[newID] {
			return nil, fmt.Errorf("新 id 冲突：项目 %d（%s）与目标 %q 映射到同一值 %q", r.ID, r.Name, targetID, newID)
		}
		seen[newID] = true
		mapping[r.ID] = newID
	}
	return mapping, nil
}

// backupSnapshot 导出 project + 引用表全部数据为 JSON 快照（回滚用）。
// 返回备份文件绝对路径。
func backupSnapshot(db *gorm.DB, dir string) (string, error) {
	snap := map[string][]map[string]interface{}{}
	all := append([]string{"project"}, refTables...)
	for _, t := range all {
		var rows []map[string]interface{}
		if err := db.Table(t).Scan(&rows).Error; err != nil {
			return "", fmt.Errorf("读取 %s 失败: %w", t, err)
		}
		snap[t] = rows
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("project_id_migration_backup_%d.json", time.Now().Unix()))
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// RunProjectID 执行 project id 字符串化迁移。
// workDir 为备份文件输出目录（通常为软件根目录）。
func RunProjectID(cfg *config.Database, workDir, targetName, targetID string) error {
	if strings.TrimSpace(targetID) == "" {
		targetID = "100001"
	}
	db, err := Connect(cfg)
	if err != nil {
		return err
	}

	// 0. 已迁移检测（project.id 已是 varchar → 直接跳过，避免重复 ALTER）
	if migrated, err := alreadyMigrated(db); err != nil {
		return err
	} else if migrated {
		fmt.Println("检测到 project.id 已是字符串类型，已迁移过，跳过。")
		return nil
	}

	// 1. 备份
	backupPath, err := backupSnapshot(db, workDir)
	if err != nil {
		return err
	}
	fmt.Printf("[1/4] 快照已备份: %s\n", backupPath)

	// 2. 计算映射
	var rows []ProjectRow
	if err := db.Model(&struct{}{}).Table("project").Order("id ASC").Scan(&rows).Error; err != nil {
		return fmt.Errorf("读取 project 失败: %w", err)
	}
	mapping, err := BuildMapping(rows, targetName, targetID)
	if err != nil {
		return err
	}
	// 展示映射
	ids := make([]uint64, 0, len(mapping))
	for k := range mapping {
		ids = append(ids, k)
	}
	sortUint64(ids)
	for _, old := range ids {
		fmt.Printf("[2/4] 项目 %d → %s\n", old, mapping[old])
	}

	// 3. 列类型改 VARCHAR(6)（MySQL DDL 不可回滚，先类型后数据）
	fmt.Println("[3/4] 正在修改列类型...")
	if err := db.Exec("ALTER TABLE project MODIFY COLUMN id VARCHAR(6) NOT NULL").Error; err != nil {
		return fmt.Errorf("修改 project.id 类型失败: %w", err)
	}
	for _, t := range refTables {
		if err := db.Exec(fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN project_id VARCHAR(6) NOT NULL", t)).Error; err != nil {
			return fmt.Errorf("修改 %s.project_id 类型失败: %w", t, err)
		}
	}

	// 4. 数据迁移（事务内更新）
	fmt.Println("[4/4] 正在迁移数据...")
	if err := db.Transaction(func(tx *gorm.DB) error {
		for old, new := range mapping {
			oldS := fmt.Sprintf("%d", old)
			if err := tx.Exec("UPDATE project SET id = ? WHERE id = ?", new, oldS).Error; err != nil {
				return fmt.Errorf("更新 project %d 失败: %w", old, err)
			}
			for _, t := range refTables {
				if err := tx.Exec(fmt.Sprintf("UPDATE %s SET project_id = ? WHERE project_id = ?", t), new, oldS).Error; err != nil {
					return fmt.Errorf("更新 %s.project_id=%d 失败: %w", t, old, err)
				}
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// 校验：孤儿引用（引用表 project_id 在 project.id 中不存在）。
	// 迁移只做「已有值 → 新值」的映射，不会新增孤儿；历史硬删项目遗留的旧引用仅作警告提示。
	var orphans int64
	for _, t := range refTables {
		var rows []struct {
			ProjectID string
			N         int
		}
		if err := db.Raw(fmt.Sprintf(
			"SELECT project_id, COUNT(*) AS n FROM %s WHERE project_id NOT IN (SELECT id FROM project) GROUP BY project_id", t)).
			Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			orphans += int64(r.N)
			fmt.Printf("⚠ 警告: %s.project_id=%q 无对应项目（历史遗留，不影响迁移）条数=%d\n", t, r.ProjectID, r.N)
		}
	}

	var projectCount int64
	db.Model(&struct{}{}).Table("project").Count(&projectCount)
	if orphans > 0 {
		fmt.Printf("迁移完成（含 %d 条历史孤儿引用警告）。project 共 %d 条。备份: %s\n", orphans, projectCount, backupPath)
	} else {
		fmt.Printf("迁移完成：project 共 %d 条，引用无孤儿。备份: %s\n", projectCount, backupPath)
	}
	return nil
}

func sortUint64(a []uint64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}
