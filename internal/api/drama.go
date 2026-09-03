package api

// 追剧转存任务的 HTTP 处理器（来自Trae）。
// 复用 ensureServiceReady / decodeJSON / pathID / writeOK / writeErr 等通用助手，
// 与 automation.go 的风格保持一致。

import (
	"net/http"
	"strconv"

	"litepan/internal/domain"
)

// dramaTaskDTO 追剧任务的对外响应结构（来自Trae）
type dramaTaskDTO struct {
	ID                     int64  `json:"id"`
	TaskName               string `json:"task_name"`
	AccountID              int64  `json:"account_id"`
	ShareURL               string `json:"share_url"`
	SavePath               string `json:"save_path"`
	Pattern                string `json:"pattern"`
	Replace                string `json:"replace"`
	IgnoreExtension        bool   `json:"ignore_extension"`
	RunWeek                string `json:"run_week"`
	EndDate                string `json:"end_date"`
	UpdateSubdir           string `json:"update_subdir"`
	UpdateSubdirResaveMode string `json:"update_subdir_resave_mode"`
	StartFID               string `json:"start_fid"`
	SortIndex              int    `json:"sort_index"`
	Status                 string `json:"status"`
	LastRunAt              string `json:"last_run_at,omitempty"`
	LastRunStatus          string `json:"last_run_status,omitempty"`
	LastRunMessage         string `json:"last_run_message,omitempty"`
	LastTreeSummary        string `json:"last_tree_summary,omitempty"`
	CreatedAt              string `json:"created_at,omitempty"`
	UpdatedAt              string `json:"updated_at,omitempty"`
}

// dramaTaskInput 创建/更新任务的入参（来自Trae）
type dramaTaskInput struct {
	TaskName               string `json:"task_name"`
	AccountID              int64  `json:"account_id"`
	ShareURL               string `json:"share_url"`
	SavePath               string `json:"save_path"`
	Pattern                string `json:"pattern"`
	Replace                string `json:"replace"`
	IgnoreExtension        bool   `json:"ignore_extension"`
	RunWeek                string `json:"run_week"`
	EndDate                string `json:"end_date"`
	UpdateSubdir           string `json:"update_subdir"`
	UpdateSubdirResaveMode string `json:"update_subdir_resave_mode"`
	StartFID               string `json:"start_fid"`
	SortIndex              int    `json:"sort_index"`
	Status                 string `json:"status"`
}

// dramaTaskRunDTO 执行历史响应结构（来自Trae）
type dramaTaskRunDTO struct {
	ID            int64  `json:"id"`
	TaskID        int64  `json:"task_id"`
	Status        string `json:"status"`
	Message       string `json:"message,omitempty"`
	TreeSummary   string `json:"tree_summary,omitempty"`
	TransferCount int    `json:"transfer_count"`
	StartedAt     string `json:"started_at,omitempty"`
	FinishedAt    string `json:"finished_at,omitempty"`
}

func toDramaTaskDTO(t *domain.DramaTask) dramaTaskDTO {
	dto := dramaTaskDTO{
		ID:                     t.ID,
		TaskName:               t.TaskName,
		AccountID:              t.AccountID,
		ShareURL:               t.ShareURL,
		SavePath:               t.SavePath,
		Pattern:                t.Pattern,
		Replace:                t.Replace,
		IgnoreExtension:        t.IgnoreExtension,
		RunWeek:                t.RunWeek,
		EndDate:                t.EndDate,
		UpdateSubdir:           t.UpdateSubdir,
		UpdateSubdirResaveMode: t.UpdateSubdirResaveMode,
		StartFID:               t.StartFID,
		SortIndex:              t.SortIndex,
		Status:                 t.Status,
		LastRunAt:              t.LastRunAt,
		LastRunStatus:          t.LastRunStatus,
		LastRunMessage:         t.LastRunMessage,
		LastTreeSummary:        t.LastTreeSummary,
	}
	if !t.CreatedAt.IsZero() {
		dto.CreatedAt = FormatAPITime(t.CreatedAt)
	}
	if !t.UpdatedAt.IsZero() {
		dto.UpdatedAt = FormatAPITime(t.UpdatedAt)
	}
	return dto
}

func toDramaTaskRunDTO(r *domain.DramaTaskRun) dramaTaskRunDTO {
	dto := dramaTaskRunDTO{
		ID:            r.ID,
		TaskID:        r.TaskID,
		Status:        r.Status,
		Message:       r.Message,
		TreeSummary:   r.TreeSummary,
		TransferCount: r.TransferCount,
	}
	if !r.StartedAt.IsZero() {
		dto.StartedAt = FormatAPITime(r.StartedAt)
	}
	if !r.FinishedAt.IsZero() {
		dto.FinishedAt = FormatAPITime(r.FinishedAt)
	}
	return dto
}

// applyInput 将入参写入领域对象，用于创建/更新（来自Trae）
func applyInput(t *domain.DramaTask, in dramaTaskInput) {
	t.TaskName = in.TaskName
	t.AccountID = in.AccountID
	t.ShareURL = in.ShareURL
	t.SavePath = in.SavePath
	t.Pattern = in.Pattern
	t.Replace = in.Replace
	t.IgnoreExtension = in.IgnoreExtension
	t.RunWeek = in.RunWeek
	t.EndDate = in.EndDate
	t.UpdateSubdir = in.UpdateSubdir
	t.UpdateSubdirResaveMode = in.UpdateSubdirResaveMode
	t.StartFID = in.StartFID
	t.SortIndex = in.SortIndex
	if in.Status != "" {
		t.Status = in.Status
	} else if t.Status == "" {
		t.Status = "running" // 默认启用（来自Trae）
	}
}

func (h *Handler) listDramaTasks(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	tasks, err := h.drama.ListTasks(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]dramaTaskDTO, 0, len(tasks))
	for _, t := range tasks {
		if t == nil {
			continue
		}
		out = append(out, toDramaTaskDTO(t))
	}
	writeOK(w, out)
}

func (h *Handler) createDramaTask(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	var in dramaTaskInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	task := &domain.DramaTask{}
	applyInput(task, in)
	id, err := h.drama.CreateTask(r.Context(), task)
	if err != nil {
		writeErr(w, err)
		return
	}
	task.ID = id
	writeOK(w, toDramaTaskDTO(task))
}

func (h *Handler) getDramaTask(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	task, err := h.drama.GetTask(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if task == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "追剧任务不存在"))
		return
	}
	writeOK(w, toDramaTaskDTO(task))
}

func (h *Handler) updateDramaTask(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	var in dramaTaskInput
	if err := decodeJSON(r, &in); err != nil {
		writeErr(w, err)
		return
	}
	task, err := h.drama.GetTask(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	if task == nil {
		writeErr(w, domain.Errorf(domain.CodeNotFound, "追剧任务不存在"))
		return
	}
	applyInput(task, in)
	task.ID = id
	if err := h.drama.UpdateTask(r.Context(), task); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, toDramaTaskDTO(task))
}

func (h *Handler) deleteDramaTask(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := h.drama.DeleteTask(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	writeOK(w, map[string]any{"id": id})
}

// runDramaTask 手动触发执行，默认 allowOnce=true 跳过星期/截止校验（来自Trae）。
// 传入 ?once=false 时按调度规则校验。异步执行，立即返回提交结果。
func (h *Handler) runDramaTask(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	allowOnce := true
	if raw := r.URL.Query().Get("once"); raw != "" {
		if v, convErr := strconv.ParseBool(raw); convErr == nil {
			allowOnce = v
		}
	}
	data := h.drama.RunTaskAsync(id, allowOnce)
	writeOK(w, data)
}

func (h *Handler) listDramaTaskRuns(w http.ResponseWriter, r *http.Request) {
	if !ensureServiceReady(w, h.drama != nil) {
		return
	}
	id, err := pathID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	limit := 20
	if raw := r.URL.Query().Get("limit"); raw != "" {
		got, convErr := strconv.Atoi(raw)
		if convErr != nil {
			writeErr(w, domain.Errorf(domain.CodeValidation, "非法 limit：%s", raw))
			return
		}
		limit = got
	}
	runs, err := h.drama.ListTaskRuns(r.Context(), id, limit)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]dramaTaskRunDTO, 0, len(runs))
	for _, run := range runs {
		if run == nil {
			continue
		}
		out = append(out, toDramaTaskRunDTO(run))
	}
	writeOK(w, out)
}
