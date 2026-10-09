// Package logclean 调用记录的定期清理。
//
// 为什么需要：`call_log` 是每次调用一行，长期会无限增长（生产上按天算）。
// 清理策略是**只清调用记录**：
//   - call_log：按天数清理（保留期默认 90 天），分批删，避免一次删太多把库锁住；
//   - billing_log（扣费账本）**永不自动删除**——那是钱相关的流水，行数也远小于调用记录。
package logclean

import (
	"log"
	"time"

	"xhl-server/internal/database"
	"xhl-server/internal/model"
)

// DefaultRetainDays 未配置时的调用记录保留天数。
const DefaultRetainDays = 90

// batchSize 每批删除条数（分批发 SQL，避免长事务/大锁）。
const batchSize = 5000

// CleanOnce 删除早于 retainDays 天的调用记录，返回删除条数。
func CleanOnce(retainDays int) (int64, error) {
	if retainDays <= 0 {
		retainDays = DefaultRetainDays
	}
	cutoff := time.Now().AddDate(0, 0, -retainDays)
	var total int64
	for {
		res := database.DB.Where("created_at < ?", cutoff).Limit(batchSize).Delete(&model.CallLog{})
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if res.RowsAffected < batchSize {
			return total, nil
		}
	}
}

// Start 启动时清一次，然后每 24 小时清一次（后台 goroutine，不影响服务启动）。
func Start(retainDays int) {
	if retainDays <= 0 {
		retainDays = DefaultRetainDays
	}
	go func() {
		// 启动后等一会儿再清，避免和 AutoMigrate/流量高峰挤在一起
		time.Sleep(30 * time.Second)
		for {
			n, err := CleanOnce(retainDays)
			if err != nil {
				log.Printf("调用记录清理失败: %v", err)
			} else if n > 0 {
				log.Printf("调用记录清理完成：删除 %d 条（保留最近 %d 天）", n, retainDays)
			}
			time.Sleep(24 * time.Hour)
		}
	}()
}
