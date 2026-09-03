package dramatransfer

// 追剧转存服务层：任务 CRUD + 定时调度 + 单次执行（来自Trae）。
// 移植自 CASX 的调度与执行入口，适配 LitePan 的 driverexec + eventbus 架构。

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"litepan/internal/domain"
	"litepan/internal/driver"
	"litepan/internal/eventbus"
	"litepan/internal/settings"
	"litepan/internal/startupwait"
)

// Executor 是 driverexec.Executor 的最小接口约束（来自Trae）
type Executor interface {
	Run(context.Context, int64, func(driver.Driver) error) error
}

// Options 服务构造参数（来自Trae）
type Options struct {
	Exec     Executor
	Repo     domain.DramaTaskRepository
	Accounts domain.AccountRepository
	Rules    domain.MagicRegexRuleRepository // 命名正则规则仓储（来自Trae）
	Bus      *eventbus.Bus
	Log      *slog.Logger
	Settings *settings.Service // 全局设置（转存调度开关/Cron），来自Trae
}

// Service 追剧转存服务（来自Trae）
type Service struct {
	exec     Executor
	repo     domain.DramaTaskRepository
	accounts domain.AccountRepository
	rules    domain.MagicRegexRuleRepository // 命名正则规则（来自Trae）
	bus      *eventbus.Bus
	log      *slog.Logger
	settings *settings.Service // 全局设置（转存调度开关/Cron），来自Trae

	mu          sync.Mutex
	started     bool
	appCtx      context.Context
	startupGate <-chan struct{}

	runMu   sync.Mutex
	running map[int64]struct{} // 正在执行的任务 ID 集合，防止并发重入
}

const schedulerInterval = 30 * time.Second
const startupDelayAfterAuth = 15 * time.Second
const taskTimeout = 10 * time.Minute

// New 构造服务（来自Trae）
func New(opts Options) *Service {
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		exec:     opts.Exec,
		repo:     opts.Repo,
		accounts: opts.Accounts,
		rules:    opts.Rules,
		bus:      opts.Bus,
		log:      log,
		settings: opts.Settings,
		running:  map[int64]struct{}{},
	}
}

// SetStartupGate 注入启动闸门（来自Trae）
func (s *Service) SetStartupGate(gate <-chan struct{}) {
	s.mu.Lock()
	s.startupGate = gate
	s.mu.Unlock()
}

// Start 启动定时调度协程（来自Trae）
func (s *Service) Start(ctx context.Context) {
	if s == nil || s.repo == nil {
		return
	}
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return
	}
	s.started = true
	s.appCtx = ctx
	gate := s.startupGate
	s.mu.Unlock()

	go func() {
		if !startupwait.Ready(ctx, gate) {
			return
		}
		if gate != nil && !startupwait.Delay(ctx, startupDelayAfterAuth) {
			return
		}
		s.schedulerLoop(ctx)
	}()
}

func (s *Service) schedulerLoop(ctx context.Context) {
	ticker := time.NewTicker(schedulerInterval)
	defer ticker.Stop()
	s.scheduleOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scheduleOnce(ctx)
		}
	}
}

// scheduleOnce 遍历运行中的任务，执行到期的（来自Trae）
func (s *Service) scheduleOnce(ctx context.Context) {
	// 全局调度开关检查，来自Trae
	if s.settings != nil && !s.settings.Bool(settings.KeyDramaSchedulerEnabled) {
		return
	}
	// Cron 匹配检查：只有在 cron 表达式匹配的分钟才触发，来自Trae
	if s.settings != nil {
		crontab := s.settings.String(settings.KeyDramaSchedulerCrontab)
		if crontab != "" && !cronMatch(crontab, time.Now()) {
			return
		}
	}
	tasks, err := s.repo.ListActive(ctx)
	if err != nil {
		s.log.Warn("drama scheduler list failed", "err", err)
		return
	}
	now := time.Now()
	for _, task := range tasks {
		if task == nil {
			continue
		}
		if err := ValidateSchedule(task, false, now); err != nil {
			continue // 不在运行星期或已过期，跳过
		}
		// 防止同一天重复执行：检查 last_run_at
		if isRunToday(task.LastRunAt, now) {
			continue
		}
		go s.runTaskSafe(context.WithoutCancel(ctx), task.ID, false)
	}
}

// isRunToday 检查上次执行是否在今天（来自Trae）
func isRunToday(lastRunAt string, now time.Time) bool {
	if lastRunAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, lastRunAt)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", lastRunAt)
		if err != nil {
			return false
		}
	}
	// time.Time.Date() 返回多值，不能直接比较；按日期字符串比对（来自Trae）
	return t.Format("2006-01-02") == now.Format("2006-01-02")
}

// RunTask 手动触发执行单个任务（来自Trae）。
// allowOnce=true 时跳过星期/截止日期校验。
func (s *Service) RunTask(ctx context.Context, taskID int64, allowOnce bool) error {
	return s.runTaskSafe(ctx, taskID, allowOnce)
}

// RunTaskAsync 异步触发执行单个任务，立即返回不阻塞调用方（来自Trae）。
// 适用于 HTTP 手动触发场景，避免长耗时转存占用请求线程。
func (s *Service) RunTaskAsync(taskID int64, allowOnce bool) map[string]any {
	parent := s.appCtx
	if parent == nil {
		parent = context.Background()
	}
	go func() {
		ctx, cancel := context.WithTimeout(parent, taskTimeout)
		defer cancel()
		if err := s.runTaskSafe(ctx, taskID, allowOnce); err != nil && s.log != nil {
			s.log.Warn("drama async run failed", "task_id", taskID, "err", err)
		}
	}()
	return map[string]any{
		"task_id":    taskID,
		"submitted":  true,
		"allow_once": allowOnce,
		"message":    "任务已提交，正在后台执行转存",
	}
}

// runTaskSafe 执行任务并捕获 panic，确保不崩溃调度协程（来自Trae）
func (s *Service) runTaskSafe(ctx context.Context, taskID int64, allowOnce bool) (retErr error) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("drama task panicked", "task_id", taskID, "panic", r)
			retErr = fmt.Errorf("drama task panic: %v", r)
		}
	}()
	return s.runTask(ctx, taskID, allowOnce)
}

// runTask 执行单个追剧任务的完整流程（来自Trae）。
// 1. 加载任务 → 2. 调度校验 → 3. 创建 run 记录 → 4. driverexec 获取驱动 →
// 5. DramaExecutor.Execute → 6. 更新 run/task → 7. 发通知
func (s *Service) runTask(ctx context.Context, taskID int64, allowOnce bool) error {
	// 防并发重入（来自Trae）
	s.runMu.Lock()
	if _, ok := s.running[taskID]; ok {
		s.runMu.Unlock()
		return domain.Errorf(domain.CodeValidation, "任务正在执行中")
	}
	s.running[taskID] = struct{}{}
	s.runMu.Unlock()
	defer func() {
		s.runMu.Lock()
		delete(s.running, taskID)
		s.runMu.Unlock()
	}()

	// 加载任务（来自Trae）
	task, err := s.repo.Get(ctx, taskID)
	if err != nil {
		return err
	}
	if task == nil {
		return domain.Errorf(domain.CodeNotFound, "追剧任务不存在")
	}

	// 调度校验（来自Trae）
	now := time.Now()
	if !allowOnce {
		if err := ValidateSchedule(task, false, now); err != nil {
			s.log.Info("drama task skipped", "task_id", taskID, "reason", err.Error())
			return nil
		}
	}

	// 创建 run 记录（来自Trae）
	run := &domain.DramaTaskRun{
		TaskID:    taskID,
		Status:    "running",
		StartedAt: now,
	}
	runID, err := s.repo.CreateRun(ctx, run)
	if err != nil {
		s.log.Warn("drama create run failed", "task_id", taskID, "err", err)
	}

	// 设置超时（来自Trae）
	runCtx, cancel := context.WithTimeout(ctx, taskTimeout)
	defer cancel()

	// 通过 driverexec 获取驱动并执行转存（来自Trae）
	var treeSummary string
	var transferCount int
	var execLog string
	// 读取启用的命名正则覆盖，注入执行器（来自Trae）
	overrides, err := s.enabledRegexOverrides(ctx)
	if err != nil {
		s.log.Warn("drama load regex overrides failed", "task_id", taskID, "err", err)
	}
	execErr := s.exec.Run(runCtx, task.AccountID, func(drv driver.Driver) error {
		executor, err := NewDramaExecutor(drv, task, overrides)
		if err != nil {
			return err
		}
		treeSummary, err = executor.Execute(runCtx)
		if err != nil {
			execLog = executor.Log()
			return err
		}
		transferCount = executor.TransferCount()
		execLog = executor.Log()
		return nil
	})

	// 更新 run 记录（来自Trae）
	finishedAt := time.Now()
	run.ID = runID
	run.FinishedAt = finishedAt
	run.TreeSummary = treeSummary
	run.TransferCount = transferCount
	if execErr != nil {
		run.Status = "failed"
		run.Message = truncateMessage(execErr.Error(), 2000)
		if execLog != "" {
			run.Message += "\n" + truncateMessage(execLog, 2000)
		}
	} else if transferCount == 0 {
		run.Status = "skipped"
		run.Message = "无可转存文件"
	} else {
		run.Status = "success"
		run.Message = fmt.Sprintf("转存 %d 个文件", transferCount)
	}
	if runID > 0 {
		if err := s.repo.UpdateRun(ctx, run); err != nil {
			s.log.Warn("drama update run failed", "run_id", runID, "err", err)
		}
	}

	// 更新 task 的 last_run 状态（来自Trae）
	task.LastRunAt = now.Format(time.RFC3339)
	task.LastRunStatus = run.Status
	task.LastRunMessage = truncateMessage(run.Message, 500)
	task.LastTreeSummary = truncateMessage(treeSummary, 5000)
	if err := s.repo.Update(ctx, task); err != nil {
		s.log.Warn("drama update task failed", "task_id", taskID, "err", err)
	}

	// 发送通知（来自Trae）
	s.publishNotification(ctx, task, run.Status, transferCount, execErr)

	if execErr != nil {
		return execErr
	}
	return nil
}

// publishNotification 发布转存完成通知（来自Trae）
// 使用独立 context，避免转存任务超时/cancel 后通知也被取消
func (s *Service) publishNotification(_ context.Context, task *domain.DramaTask, status string, count int, execErr error) {
	if s.bus == nil {
		return
	}
	level := "info"
	title := "追剧转存：" + task.TaskName
	message := ""
	switch status {
	case "success":
		message = fmt.Sprintf("转存完成，共 %d 个文件", count)
	case "skipped":
		level = "info"
		message = "无可转存文件"
	case "failed":
		level = "error"
		if execErr != nil {
			message = truncateMessage(execErr.Error(), 500)
		} else {
			message = "转存失败"
		}
	}
	// 用 context.Background() 避免任务 ctx cancel 后通知持久化失败，来自Trae
	s.bus.Publish(context.Background(), eventbus.NotificationCreated{
		Level:     level,
		Category:  "drama",
		Title:     title,
		Message:   message,
		AccountID: task.AccountID,
		RefID:     task.ID,
	})
}

// truncateMessage 截断消息到指定长度（来自Trae）
func truncateMessage(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// cronMatch 检查当前时间是否匹配 5 段式 Cron 表达式（分 时 日 月 周），来自Trae
// 支持 * / 数字 / 逗号 / 范围(-) / 步长(*/N)，与 CASX CronTrigger 行为对齐
func cronMatch(expr string, t time.Time) bool {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return true // 格式不正确时不阻断调度
	}
	minute := t.Minute()
	hour := t.Hour()
	day := t.Day()
	month := int(t.Month())
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7
	}
	return cronFieldMatch(fields[0], minute, 0, 59) &&
		cronFieldMatch(fields[1], hour, 0, 23) &&
		cronFieldMatch(fields[2], day, 1, 31) &&
		cronFieldMatch(fields[3], month, 1, 12) &&
		cronFieldMatch(fields[4], weekday, 0, 7)
}

// cronFieldMatch 检查单个字段是否匹配，来自Trae
func cronFieldMatch(field string, val, min, max int) bool {
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "*" {
			return true
		}
		// */N 步长
		if strings.HasPrefix(part, "*/") {
			step, err := strconv.Atoi(part[2:])
			if err != nil || step <= 0 {
				continue
			}
			if val%step == 0 {
				return true
			}
			continue
		}
		// N-M 范围
		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			lo, err1 := strconv.Atoi(rangeParts[0])
			hi, err2 := strconv.Atoi(rangeParts[1])
			if err1 != nil || err2 != nil {
				continue
			}
			if val >= lo && val <= hi {
				return true
			}
			continue
		}
		// N 单值
		n, err := strconv.Atoi(part)
		if err != nil {
			continue
		}
		if val == n {
			return true
		}
	}
	return false
}

// === CRUD 方法（来自Trae）===

// CreateTask 创建追剧任务（来自Trae）
func (s *Service) CreateTask(ctx context.Context, task *domain.DramaTask) (int64, error) {
	if task == nil {
		return 0, domain.Errorf(domain.CodeValidation, "无效追剧任务")
	}
	if task.TaskName == "" {
		return 0, domain.Errorf(domain.CodeValidation, "任务名称不能为空")
	}
	if task.ShareURL == "" {
		return 0, domain.Errorf(domain.CodeValidation, "分享链接不能为空")
	}
	if task.AccountID == 0 {
		return 0, domain.Errorf(domain.CodeValidation, "请选择存储账号")
	}
	return s.repo.Create(ctx, task)
}

// UpdateTask 更新追剧任务（来自Trae）
func (s *Service) UpdateTask(ctx context.Context, task *domain.DramaTask) error {
	if task == nil || task.ID == 0 {
		return domain.Errorf(domain.CodeValidation, "无效追剧任务")
	}
	return s.repo.Update(ctx, task)
}

// DeleteTask 删除追剧任务（来自Trae）
func (s *Service) DeleteTask(ctx context.Context, id int64) error {
	return s.repo.Delete(ctx, id)
}

// GetTask 获取追剧任务（来自Trae）
func (s *Service) GetTask(ctx context.Context, id int64) (*domain.DramaTask, error) {
	return s.repo.Get(ctx, id)
}

// ListTasks 列出全部追剧任务（来自Trae）
func (s *Service) ListTasks(ctx context.Context) ([]*domain.DramaTask, error) {
	return s.repo.List(ctx)
}

// ListTaskRuns 列出任务的执行历史（来自Trae）
func (s *Service) ListTaskRuns(ctx context.Context, taskID int64, limit int) ([]*domain.DramaTaskRun, error) {
	return s.repo.ListRuns(ctx, taskID, limit)
}
