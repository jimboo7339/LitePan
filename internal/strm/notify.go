package strm

import (
	"context"
	"fmt"
	"strings"

	"litepan/internal/domain"
	"litepan/internal/eventbus"
)

// notifyScanSuccess STRM 扫描成功完成通知（来自Trae）。
// 只有真正有变更（新增/更新/清理）时才推送，避免零变更刷屏。
func (s *Service) notifyScanSuccess(task *domain.StrmTask, result *ScanResult) {
	if s == nil || s.bus == nil || task == nil || result == nil {
		return
	}
	// 没有任何变更不发通知（来自Trae）
	if result.GeneratedCount == 0 && result.UpdatedCount == 0 && result.RemovedCount == 0 {
		return
	}
	summaryParts := []string{}
	if result.GeneratedCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("新增 %d 个 STRM", result.GeneratedCount))
	}
	if result.UpdatedCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("更新 %d 个", result.UpdatedCount))
	}
	if result.RemovedCount > 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("清理 %d 个", result.RemovedCount))
	}
	message := fmt.Sprintf("✅ STRM任务「%s」扫描完成：%s", task.Name, strings.Join(summaryParts, "，"))
	if result.ScannedCount > 0 {
		message += fmt.Sprintf("\n扫描文件总数：%d", result.ScannedCount)
	}
	s.bus.Publish(context.Background(), eventbus.NotificationCreated{
		Level:     "success",
		Category:  "strm",
		Title:     "【STRM 任务】",
		Message:   message,
		AccountID: task.AccountID,
		RefID:     task.ID,
	})
}

func (s *Service) notifyScanFailures(task *domain.StrmTask, failures []ScanFailure) {
	if s == nil || s.bus == nil || task == nil || len(failures) == 0 {
		return
	}
	summary := scanFailureSummary(task.Name, failures)
	s.bus.Publish(context.Background(), eventbus.NotificationCreated{
		Level:     "warning",
		Category:  domain.NotificationCategoryStrmScanWarn,
		Title:     "STRM 扫描部分失败",
		Message:   EncodeScanFailureMessage(summary, failures),
		AccountID: task.AccountID,
		RefID:     task.ID,
	})
}

// notifyScanProtected 安全保护阻止本地清理时通知用户，附上原因。
func (s *Service) notifyScanProtected(task *domain.StrmTask, reason string) {
	if s == nil || s.bus == nil || task == nil || strings.TrimSpace(reason) == "" {
		return
	}
	s.bus.Publish(context.Background(), eventbus.NotificationCreated{
		Level:     "warning",
		Category:  domain.NotificationCategoryStrmScanWarn,
		Title:     "STRM 扫描安全保护阻止清理",
		Message:   fmt.Sprintf("任务「%s」：%s", task.Name, reason),
		AccountID: task.AccountID,
		RefID:     task.ID,
	})
}
