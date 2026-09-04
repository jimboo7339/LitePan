package strm

import (
	"context"
	"fmt"
	"strings"

	"litepan/internal/auth"
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
	message := fmt.Sprintf("STRM任务「%s」扫描完成：%s", task.Name, strings.Join(summaryParts, "，"))
	if result.ScannedCount > 0 {
		message += fmt.Sprintf("\n扫描文件总数：%d", result.ScannedCount)
	}
	s.bus.Publish(context.Background(), eventbus.NotificationCreated{
		Level:     "success",
		Category:  "strm",
		Title:     fmt.Sprintf("【STRM 任务】%s 扫描完成", task.Name),
		Message:   message,
		AccountID: task.AccountID,
		RefID:     task.ID,
	})
}

// notifyScanFailure STRM 扫描整体失败（非认证类）时的通知（来自Trae）。
// 与 notifyScanFailures 不同：那个是"扫描成功但有单文件失败"，这个是整次扫描异常中断。
func (s *Service) notifyScanFailure(task *domain.StrmTask, cause error) {
	if s == nil || s.bus == nil || task == nil || cause == nil {
		return
	}
	// 认证错误会走 PauseTask 通知链路，这里不发避免重复（来自Trae）
	if auth.IsAuthError(cause) {
		return
	}
	s.bus.Publish(context.Background(), eventbus.NotificationCreated{
		Level:     "error",
		Category:  domain.NotificationCategoryStrmScanWarn,
		Title:     fmt.Sprintf("【STRM 任务】%s 扫描失败", task.Name),
		Message:   fmt.Sprintf("STRM任务「%s」扫描失败：%s", task.Name, cause.Error()),
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
