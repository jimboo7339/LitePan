import { http } from "./client";

// 追剧转存任务 API 封装（来自Trae）。
// 对应后端 internal/api/drama.go 的 /api/admin/drama/* 路由。

// DramaTask 追剧转存任务（来自Trae）
export interface DramaTask {
  id: number;
  task_name: string;
  account_id: number;
  share_url: string;
  save_path: string;
  pattern: string;
  replace: string;
  ignore_extension: boolean;
  run_week: string; // 逗号分隔的 1-7
  end_date: string; // YYYY-MM-DD
  update_subdir: string;
  update_subdir_resave_mode: string;
  start_fid: string;
  sort_index: number;
  status: string; // running/paused
  last_run_at?: string;
  last_run_status?: string;
  last_run_message?: string;
  last_tree_summary?: string;
  created_at?: string;
  updated_at?: string;
}

// DramaTaskInput 创建/更新任务入参（来自Trae）
export interface DramaTaskInput {
  task_name: string;
  account_id: number;
  share_url: string;
  save_path: string;
  pattern: string;
  replace: string;
  ignore_extension: boolean;
  run_week: string;
  end_date: string;
  update_subdir: string;
  update_subdir_resave_mode: string;
  start_fid: string;
  sort_index: number;
  status: string;
}

// DramaTaskRun 执行历史（来自Trae）
export interface DramaTaskRun {
  id: number;
  task_id: number;
  status: string; // running/success/failed/skipped
  message?: string;
  tree_summary?: string;
  transfer_count: number;
  started_at?: string;
  finished_at?: string;
}

// RunDramaTaskResult 手动触发执行结果（来自Trae）
export interface RunDramaTaskResult {
  submitted: boolean;
  task_id: number;
  allow_once: boolean;
  message: string;
}

export function fetchDramaTasks() {
  return http.get<DramaTask[]>("/admin/drama/tasks");
}

export function createDramaTask(body: DramaTaskInput) {
  return http.post<DramaTask>("/admin/drama/tasks", body);
}

export function getDramaTask(id: number) {
  return http.get<DramaTask>(`/admin/drama/tasks/${id}`);
}

export function updateDramaTask(id: number, body: DramaTaskInput) {
  return http.put<DramaTask>(`/admin/drama/tasks/${id}`, body);
}

export function deleteDramaTask(id: number) {
  return http.del<{ id: number }>(`/admin/drama/tasks/${id}`);
}

// runDramaTask 手动触发执行一次（来自Trae）。
// once=true 跳过星期/截止校验；默认 true。
export function runDramaTask(id: number, once = true) {
  return http.post<RunDramaTaskResult>(`/admin/drama/tasks/${id}/run`, {}, { once });
}

export function fetchDramaTaskRuns(id: number, limit = 20) {
  return http.get<DramaTaskRun[]>(`/admin/drama/tasks/${id}/runs`, { limit });
}

// === 命名正则规则维护（来自Trae，对应 /admin/drama/rules） ===

// MagicRegexRule 命名正则规则（来自Trae）
export interface MagicRegexRule {
  key: string; // 以 $ 开头的规则键
  label: string; // 展示名称
  enabled: boolean; // 是否启用
  built_in: boolean; // 是否为内置规则
  overridden: boolean; // 内置规则是否被覆盖
  pattern: string; // 匹配正则
  replace: string; // 替换模板
  default_pattern?: string; // 内置默认 pattern（仅内置规则）
  default_replace?: string; // 内置默认 replace（仅内置规则）
}

// MagicRegexRuleInput 规则写入入参（来自Trae）
export interface MagicRegexRuleInput {
  label?: string | null;
  enabled?: boolean;
  pattern?: string | null;
  replace?: string | null;
}

// fetchMagicRegexRules 获取全部规则（内置 + 自定义）（来自Trae）
export function fetchMagicRegexRules() {
  return http.get<{ rules: MagicRegexRule[] }>("/admin/drama/rules");
}

// saveMagicRegexRule 创建/覆盖规则（来自Trae），返回更新后的规则列表
export function saveMagicRegexRule(key: string, body: MagicRegexRuleInput) {
  return http.put<{ rules: MagicRegexRule[] }>(`/admin/drama/rules/${encodeURIComponent(key)}`, body);
}

// deleteMagicRegexRule 删除规则（来自Trae），返回更新后的规则列表
export function deleteMagicRegexRule(key: string) {
  return http.del<{ rules: MagicRegexRule[] }>(`/admin/drama/rules/${encodeURIComponent(key)}`);
}

// === 分享链接预览（来自Trae，对应 /admin/drama/share/preview） ===

// SharePreviewItem 分享预览条目（来自Trae）
export interface SharePreviewItem {
  fid: string;
  fid_token?: string;
  name: string; // 原始文件名
  name_re?: string; // 重命名后的目标名；为空表示不参与转存
  is_dir: boolean;
  updated_at: number;
  size: number;
  children_count: number;
  name_saved?: string; // 状态标注：已存在文件名 / 起始及之前 / 重命名冲突
}

// SharePreviewResult 分享预览结果（来自Trae）
export interface SharePreviewResult {
  drive_type: string;
  pwd_id: string;
  pdir_fid: string;
  items: SharePreviewItem[];
}

// SharePreviewInput 预览入参（来自Trae）
export interface SharePreviewInput {
  account_id: number;
  share_url: string;
  pdir_fid?: string; // 浏览层级用；空则从分享链接解析
  max_items?: number;
  task_name?: string;
  pattern?: string;
  replace?: string;
  sort_index?: number;
  save_path?: string;
  ignore_extension?: boolean;
  update_subdir?: string;
  start_fid?: string;
}

// previewShare 预览分享目录内容与重命名效果（来自Trae）
export function previewShare(body: SharePreviewInput) {
  return http.post<SharePreviewResult>("/admin/drama/share/preview", body);
}
