<script setup lang="ts">
// 追剧转存任务管理面板（来自Trae）。
// 复刻 CASX DramaTaskView / DramaTaskDrawer 的核心逻辑，
// 采用 LitePan 管理后台的面板/表格/弹窗风格。
import { computed, onMounted, reactive, ref, watch } from "vue";
import { storeToRefs } from "pinia";
import { getApiErrorMessage } from "@/api/client";
import {
  createDramaTask,
  deleteDramaTask,
  fetchDramaTaskRuns,
  fetchDramaTasks,
  runDramaTask,
  updateDramaTask,
  type DramaTask,
  type DramaTaskInput,
  type DramaTaskRun,
} from "@/api/drama";
import AppButton from "@/components/base/AppButton.vue";
import AppInput from "@/components/base/AppInput.vue";
import AppSelect from "@/components/base/AppSelect.vue";
import AppModal from "@/components/base/AppModal.vue";
import AppTabBar from "@/components/base/AppTabBar.vue";
import FormField from "@/components/base/FormField.vue";
import AdminEnableToggle from "@/components/admin/AdminEnableToggle.vue";
import AdminTableActionBtn from "@/components/admin/AdminTableActionBtn.vue";
import AdminRowActions from "@/components/admin/AdminRowActions.vue";
import AdminStatusPill from "@/components/admin/AdminStatusPill.vue";
import MagicRegexRules from "@/components/admin/MagicRegexRules.vue";
import DramaSharePreviewModal from "@/components/admin/DramaSharePreviewModal.vue";
import FolderPickerModal from "@/components/file/FolderPickerModal.vue";
import { useAccountsStore } from "@/stores/accounts";
import { useAdminPageLoading } from "@/composables/useAdminLoadingBar";
import { confirm } from "@/composables/useConfirm";
import { toast } from "@/composables/useToast";
import { formatTime } from "@/utils/format";
import "@/styles/admin-shared.css";
import "@/styles/admin-table.css";

// 内置命名正则（来自Trae），与后端 magic_rename.go 的规则名一致。
const BUILTIN_PATTERNS = [
  { key: "$TV_REGEX", label: "TV 正则（通用剧集）" },
  { key: "$TV_MAGIC", label: "TV 魔法（剧集过滤杂质）" },
  { key: "$SHOW_MAGIC", label: "综艺魔法（过滤杂质）" },
  { key: "$SHOW_PRO", label: "综艺 Pro（过滤杂质）" },
  { key: "$BLACK_WORD", label: "黑名单过滤（剔除广告/预告）" },
];

// 周几选项（来自Trae），ISO 1-7 与后端 ValidateSchedule 判定保持一致。
// 任务级"运行星期"是任务过滤器：全局 Cron 决定何时扫，勾选的周几决定这次扫要跑哪些任务。
const WEEK_OPTIONS = [
  { value: "1", label: "周一" },
  { value: "2", label: "周二" },
  { value: "3", label: "周三" },
  { value: "4", label: "周四" },
  { value: "5", label: "周五" },
  { value: "6", label: "周六" },
  { value: "7", label: "周日" },
];

const accountsStore = useAccountsStore();
const { accounts } = storeToRefs(accountsStore);
const activeAccounts = computed(() => accounts.value.filter((a) => a.is_active));

const tasks = ref<DramaTask[]>([]);
const loading = ref(false);
const listReady = ref(false);
useAdminPageLoading(
  "drama",
  computed(() => !listReady.value || loading.value),
);

// 列表搜索/状态过滤（来自Trae，复刻 CASX filteredTasks）
const filters = reactive({ keyword: "", status: "all" as "all" | "enabled" | "disabled" });
const filteredTasks = computed(() =>
  tasks.value.filter((t) => {
    const kw = filters.keyword.trim().toLowerCase();
    const matchesKeyword =
      !kw ||
      [t.task_name, t.share_url, t.save_path].filter(Boolean).some((v) => String(v).toLowerCase().includes(kw));
    const matchesStatus =
      filters.status === "all" ||
      (filters.status === "enabled" && t.status === "running") ||
      (filters.status === "disabled" && t.status === "paused");
    return matchesKeyword && matchesStatus;
  }),
);

function accountName(id: number): string {
  return accounts.value.find((a) => a.id === id)?.name || `账号 #${id}`;
}

// 页签：转存任务 / 命名规则（来自Trae）
const activeTab = ref<"tasks" | "rules">("tasks");
const props = defineProps<{ standalone?: boolean }>();
const showTabBar = computed(() => props.standalone ?? true);

// 表单弹窗状态（来自Trae）
const drawerOpen = ref(false);
const editingId = ref<number | null>(null);
const submitting = ref(false);
const pickerOpen = ref(false);

// 分享链接预览弹窗状态（来自Trae）
const previewOpen = ref(false);
// 预览使用的参数快照：从当前表单或某个任务行取值
const previewParams = reactive({
  shareUrl: "",
  taskName: "",
  pattern: "",
  replace: "",
  sortIndex: 0,
  savePath: "",
  ignoreExtension: false,
  updateSubdir: "",
  startFid: "",
});

// 用当前表单打开预览（来自Trae）
function openPreview() {
  if (!form.account_id) {
    toast.warning("请先选择保存账号");
    return;
  }
  if (!form.share_url.trim()) {
    toast.warning("请先填写分享链接");
    return;
  }
  Object.assign(previewParams, {
    shareUrl: form.share_url.trim(),
    taskName: form.task_name,
    pattern: form.pattern,
    replace: form.replace,
    sortIndex: form.sort_index,
    savePath: form.save_path,
    ignoreExtension: form.ignore_extension,
    updateSubdir: form.update_subdir,
    startFid: form.start_fid,
  });
  previewOpen.value = true;
}

// 用某个任务行的参数打开预览（来自Trae）
function openTaskPreview(task: DramaTask) {
  if (!task.account_id) {
    toast.warning("该任务没有账号，无法预览");
    return;
  }
  Object.assign(previewParams, {
    shareUrl: task.share_url,
    taskName: task.task_name,
    pattern: task.pattern,
    replace: task.replace,
    sortIndex: task.sort_index,
    savePath: task.save_path,
    ignoreExtension: task.ignore_extension,
    updateSubdir: task.update_subdir,
    startFid: task.start_fid,
  });
  previewOpen.value = true;
}

// 预览中选中了某个子目录（来自Trae）：回填到当前表单分享链接
function onPreviewSelect(payload: { shareUrl: string; fid: string; name: string }) {
  form.share_url = payload.shareUrl;
  toast.success(`已定位到分享目录：${payload.name}`);
}

const emptyForm = (): DramaTaskInput => ({
  task_name: "",
  account_id: activeAccounts.value[0]?.id ?? 0,
  share_url: "",
  save_path: "",
  pattern: "$TV_REGEX",
  replace: "",
  ignore_extension: false,
  run_week: "",
  end_date: "",
  update_subdir: "",
  update_subdir_resave_mode: "none",
  start_fid: "",
  sort_index: 0,
  status: "running",
});
const form = reactive<DramaTaskInput>(emptyForm());
// 运行星期多选（来自Trae）：数组形式，提交时按 ISO 1-7 拼成逗号分隔字符串；空数组 = 每天都能跑。
const weekSelection = ref<string[]>([]);
// 排序基数输入代理（来自Trae）：AppInput 始终 emit 字符串，
// 这里在表单态里保持 number，提交时不会被误写成字符串。
const sortIndexText = computed({
  get: () => String(form.sort_index),
  set: (v: string) => {
    const n = Number(v);
    form.sort_index = Number.isFinite(n) && n > 0 ? n : 0;
  },
});

// 切换单个星期选择（来自Trae）：与 pill 按钮配合，替代原生 checkbox
function toggleWeek(value: string) {
  const idx = weekSelection.value.indexOf(value);
  if (idx >= 0) {
    weekSelection.value.splice(idx, 1);
  } else {
    weekSelection.value.push(value);
    // 保持周序，方便后续 join 拼接（来自Trae）
    weekSelection.value.sort((a, b) => Number(a) - Number(b));
  }
}

function openCreate() {
  editingId.value = null;
  Object.assign(form, emptyForm());
  weekSelection.value = [];
  drawerOpen.value = true;
}

function openEdit(task: DramaTask) {
  editingId.value = task.id;
  Object.assign(form, {
    task_name: task.task_name,
    account_id: task.account_id,
    share_url: task.share_url,
    save_path: task.save_path,
    pattern: task.pattern,
    replace: task.replace,
    ignore_extension: task.ignore_extension,
    run_week: task.run_week ?? "",
    end_date: task.end_date,
    update_subdir: task.update_subdir,
    update_subdir_resave_mode: task.update_subdir_resave_mode,
    start_fid: task.start_fid,
    sort_index: task.sort_index,
    status: task.status,
  });
  weekSelection.value = (task.run_week ?? "")
    .split(",")
    .map((s) => s.trim())
    .filter((s) => WEEK_OPTIONS.some((w) => w.value === s));
  drawerOpen.value = true;
}

// 保存任务（来自Trae）
async function submitTask(): Promise<number | null> {
  if (!form.task_name.trim()) {
    toast.error("请填写任务名称");
    return null;
  }
  if (!form.share_url.trim()) {
    toast.error("请填写分享链接");
    return null;
  }
  if (!form.save_path.trim()) {
    toast.error("请选择保存目录");
    return null;
  }
  const payload: DramaTaskInput = {
    ...form,
    task_name: form.task_name.trim(),
    share_url: form.share_url.trim(),
    run_week: weekSelection.value.join(","),
  };
  submitting.value = true;
  try {
    if (editingId.value != null) {
      await updateDramaTask(editingId.value, payload);
      toast.success("任务已更新");
    } else {
      const created = await createDramaTask(payload);
      editingId.value = created.id;
      toast.success("任务已创建");
    }
    drawerOpen.value = false;
    await loadData();
    return editingId.value;
  } catch (e) {
    toast.error(getApiErrorMessage(e, "保存失败"));
    return null;
  } finally {
    submitting.value = false;
  }
}

// 保存并运行一次（来自Trae，复刻 CASX 的 run-once）
async function submitAndRunOnce() {
  const id = await submitTask();
  if (id == null) return;
  // triggerRun 接收的是 DramaTask，这里从列表里取回刚保存的任务（来自Trae）
  const saved = tasks.value.find((t) => t.id === id);
  if (saved) await triggerRun(saved);
}

async function triggerRun(task: DramaTask) {
  try {
    const res = await runDramaTask(task.id, true);
    toast.success(res.message || "已提交执行");
    // 稍后刷新列表查看执行结果
    window.setTimeout(() => void loadData(), 2500);
  } catch (e) {
    toast.error(getApiErrorMessage(e, "执行失败"));
  }
}

async function toggleTask(task: DramaTask, enabled: boolean) {
  const nextStatus = enabled ? "running" : "paused";
  try {
    await updateDramaTask(task.id, {
      task_name: task.task_name,
      account_id: task.account_id,
      share_url: task.share_url,
      save_path: task.save_path,
      pattern: task.pattern,
      replace: task.replace,
      ignore_extension: task.ignore_extension,
      run_week: task.run_week ?? "",
      end_date: task.end_date,
      update_subdir: task.update_subdir,
      update_subdir_resave_mode: task.update_subdir_resave_mode,
      start_fid: task.start_fid,
      sort_index: task.sort_index,
      status: nextStatus,
    });
    task.status = nextStatus;
    toast.success(enabled ? "已启用" : "已停用");
  } catch (e) {
    toast.error(getApiErrorMessage(e, "更新状态失败"));
  }
}

async function removeTask(task: DramaTask) {
  const ok = await confirm({
    title: "删除转存任务",
    message: `确定删除任务「${task.task_name}」吗？删除后无法恢复。`,
    confirmText: "删除",
    danger: true,
  });
  if (!ok) return;
  try {
    await deleteDramaTask(task.id);
    toast.success("任务已删除");
    await loadData();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "删除失败"));
  }
}

// 运行记录抽屉（来自Trae）
const runsDrawerOpen = ref(false);
const runsLoading = ref(false);
const runsTask = ref<DramaTask | null>(null);
const runs = ref<DramaTaskRun[]>([]);
const runExpanded = ref<Set<number>>(new Set());

async function openRuns(task: DramaTask) {
  runsTask.value = task;
  runsDrawerOpen.value = true;
  runsLoading.value = true;
  try {
    runs.value = await fetchDramaTaskRuns(task.id, 20);
  } catch (e) {
    toast.error(getApiErrorMessage(e, "加载运行记录失败"));
    runs.value = [];
  } finally {
    runsLoading.value = false;
  }
}

function closeRuns() {
  runsDrawerOpen.value = false;
  runsTask.value = null;
  runs.value = [];
}

function toggleRunExpanded(id: number) {
  const next = new Set(runExpanded.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  runExpanded.value = next;
}

function runStatusText(status: string): string {
  switch (status) {
    case "success":
      return "成功";
    case "failed":
      return "失败";
    case "skipped":
      return "跳过";
    case "running":
      return "执行中";
    default:
      return status || "未知";
  }
}

function lastRunLabel(task: DramaTask): string {
  switch (task.last_run_status) {
    case "success":
      return "成功";
    case "failed":
      return "失败";
    case "skipped":
      return "跳过";
    case "running":
      return "执行中";
    default:
      return "未执行";
  }
}

function onFolderPicked(payload: { accountId: number; parentId: string; path: string }) {
  form.account_id = payload.accountId;
  form.save_path = payload.path || "/";
  pickerOpen.value = false;
}

async function loadData() {
  loading.value = true;
  try {
    tasks.value = await fetchDramaTasks();
  } catch (e) {
    toast.error(getApiErrorMessage(e, "加载转存任务失败"));
  } finally {
    loading.value = false;
    listReady.value = true;
  }
}

onMounted(async () => {
  if (!accounts.value.length) await accountsStore.loadAccounts();
  await loadData();
});

watch(
  () => activeAccounts.value.map((a) => a.id).join(","),
  () => {
    if (!form.account_id && activeAccounts.value.length) {
      form.account_id = activeAccounts.value[0].id;
    }
  },
);
</script>

<template>
  <div class="drama-page">
    <AppTabBar
      v-if="showTabBar"
      v-model="activeTab"
      :tabs="[
        { key: 'tasks', label: '转存任务' },
        { key: 'rules', label: '命名规则' },
      ]"
    />

    <section v-if="activeTab === 'tasks'" class="admin-panel-table-wrap drama-list-panel">
      <div class="panel-head">
        <div class="panel-head__title">
          <div class="panel-title">
            <i class="fas fa-clapperboard panel-title__icon"></i>
            <span>转存任务</span>
          </div>
          <div class="panel-sub">追剧自动转存：填入夸克 / 光鸭分享链接，按规则定时转存到网盘目录。</div>
        </div>
        <div class="panel-head__toolbar">
          <div class="drama-filters">
            <div class="drama-search">
              <i class="fas fa-magnifying-glass drama-search__icon"></i>
              <input
                v-model="filters.keyword"
                class="drama-search__input"
                type="text"
                placeholder="搜索任务名 / 链接 / 路径"
              />
              <button
                v-if="filters.keyword"
                type="button"
                class="drama-search__clear"
                title="清空"
                @click="filters.keyword = ''"
              >
                <i class="fas fa-circle-xmark"></i>
              </button>
            </div>
            <AppSelect
              v-model="filters.status"
              :options="[
                { value: 'all', label: '全部状态' },
                { value: 'enabled', label: '已启用' },
                { value: 'disabled', label: '已停用' },
              ]"
            />
          </div>
          <div class="panel-head__divider" aria-hidden="true"></div>
          <div class="panel-head__meta">
            <span class="panel-head__badge">
              <span class="panel-head__badge-dot"></span>
              {{ filteredTasks.length }} 个任务
            </span>
            <AppButton type="button" size="sm" variant="primary" @click="openCreate">
              <i class="fas fa-plus"></i>
              新增任务
            </AppButton>
          </div>
        </div>
      </div>
      <div class="table-wrap">
        <table class="admin-table drama-table">
          <thead>
            <tr>
              <th class="col-name">任务名</th>
              <th class="col-account">账号</th>
              <th class="col-path">保存路径</th>
              <th class="col-pattern">规则</th>
              <th class="col-last">上次执行</th>
              <th class="col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="6" class="empty-cell">加载中...</td>
            </tr>
            <tr v-else-if="filteredTasks.length === 0">
              <td colspan="6" class="empty-cell">还没有转存任务，点右上角「新增任务」创建第一条规则</td>
            </tr>
            <tr v-for="task in filteredTasks" v-else :key="task.id" class="drama-row">
              <td>
                <div class="drama-name">{{ task.task_name }}</div>
                <div class="drama-desc">
                  <AdminStatusPill :tone="task.status === 'running' ? 'success' : 'muted'">
                    {{ task.status === "running" ? "启用" : "停用" }}
                  </AdminStatusPill>
                  <span v-if="task.run_week" class="drama-desc__week" :title="'仅在这些周几执行（配合全局扫描频率）'">
                    <i class="fas fa-calendar-week"></i>
                    周{{ task.run_week.split(",").join("、") }}
                  </span>
                  <span v-else class="drama-desc__week" :title="'未勾选周几，每次扫描都会尝试执行'">
                    <i class="fas fa-calendar-day"></i>
                    每日
                  </span>
                </div>
              </td>
              <td class="drama-account">{{ accountName(task.account_id) }}</td>
              <td class="drama-path" :title="task.save_path">{{ task.save_path || "-" }}</td>
              <td>
                <span class="drama-pattern" :title="task.pattern">{{ task.pattern || "无" }}</span>
              </td>
              <td class="drama-last">
                <div class="drama-last__status">{{ lastRunLabel(task) }}</div>
                <div class="drama-last__time">{{ formatTime(task.last_run_at) }}</div>
              </td>
              <td class="admin-table__actions">
                <AdminRowActions>
                  <AdminEnableToggle
                    :enabled="task.status === 'running'"
                    aria-label="转存任务启用切换"
                    @enable="(enabled) => toggleTask(task, enabled)"
                  />
                  <AdminTableActionBtn icon="play" title="运行一次" @click="triggerRun(task)" />
                  <AdminTableActionBtn icon="log" title="运行记录" @click="openRuns(task)" />
                  <AdminTableActionBtn icon="edit" title="编辑" @click="openEdit(task)" />
                  <AdminTableActionBtn icon="delete" title="删除" danger @click="removeTask(task)" />
                  <template #menu>
                    <button class="admin-row-actions__item" type="button" @click="toggleTask(task, task.status !== 'running')">
                      {{ task.status === "running" ? "停用" : "启用" }}
                    </button>
                    <button class="admin-row-actions__item" type="button" @click="triggerRun(task)">运行一次</button>
                    <button class="admin-row-actions__item" type="button" @click="openRuns(task)">运行记录</button>
                    <button class="admin-row-actions__item" type="button" @click="openTaskPreview(task)">分享预览</button>
                    <button class="admin-row-actions__item" type="button" @click="openEdit(task)">编辑</button>
                    <button class="admin-row-actions__item admin-row-actions__item--danger" type="button" @click="removeTask(task)">删除</button>
                  </template>
                </AdminRowActions>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section v-if="activeTab === 'rules'" class="admin-panel-table-wrap drama-list-panel">
      <MagicRegexRules />
    </section>

    <!-- 新增/编辑任务弹窗（来自Trae，复刻 CASX DramaTaskDrawer 字段） -->
    <AppModal
      :open="drawerOpen"
      size="lg"
      :title="editingId != null ? `编辑任务：${form.task_name}` : '新增转存任务'"
      @close="drawerOpen = false"
    >
      <div class="drama-form">
        <div class="drama-form__section">
          <div class="drama-form__section-head">
            <span class="drama-form__section-bar" aria-hidden="true"></span>
            <i class="fas fa-id-card drama-form__section-icon"></i>
            <span class="drama-form__section-title">基本信息</span>
            <span class="drama-form__section-hint">任务标识与归属账号</span>
          </div>
        </div>
        <div class="modal-form__row">
          <FormField label="任务名称" required>
            <AppInput v-model="form.task_name" placeholder="例如：某电视剧" />
          </FormField>
          <FormField label="保存账号">
            <AppSelect
              v-model="form.account_id"
              :options="activeAccounts.map((a) => ({ value: a.id, label: a.name }))"
              placeholder="请选择账号"
            />
          </FormField>
        </div>
        <div class="modal-form__row">
          <FormField label="分享链接" required>
            <div class="share-url-row">
              <AppInput v-model="form.share_url" placeholder="夸克/光鸭分享链接" />
              <AppButton
                type="button"
                size="sm"
                variant="secondary"
                :disabled="!form.account_id || !form.share_url.trim()"
                @click="openPreview"
              >
                <i class="fas fa-eye"></i>
                预览
              </AppButton>
            </div>
          </FormField>
          <FormField label="保存目录">
            <div class="save-path-row">
              <AppInput v-model="form.save_path" placeholder="例如 /影视资源/某剧，留空则保存到账号根目录" />
              <AppButton type="button" size="sm" variant="secondary" :disabled="!form.account_id" @click="pickerOpen = true">
                <i class="fas fa-folder-open"></i>
                浏览
              </AppButton>
            </div>
          </FormField>
        </div>

        <div class="drama-form__section">
          <div class="drama-form__section-head">
            <span class="drama-form__section-bar" aria-hidden="true"></span>
            <i class="fas fa-gear drama-form__section-icon"></i>
            <span class="drama-form__section-title">保存规则</span>
            <span class="drama-form__section-hint">匹配、重命名、判重与起始位置</span>
          </div>
        </div>
        <div class="modal-form__row">
          <FormField label="内置规则">
            <AppSelect
              v-model="form.pattern"
              :options="BUILTIN_PATTERNS.map((p) => ({ value: p.key, label: p.label }))"
              placeholder="选择内置规则"
            />
          </FormField>
          <FormField label="匹配表达式（pattern）">
            <AppInput v-model="form.pattern" placeholder="$TV_REGEX 或自定义正则" />
          </FormField>
        </div>
        <div class="modal-form__row">
          <FormField label="替换表达式（replace）">
            <AppInput v-model="form.replace" placeholder="例如：\1E\2.\3（留空则保持原名）" />
          </FormField>
          <FormField label="排序基数（sort_index）">
            <AppInput v-model="sortIndexText" type="number" min="1" />
          </FormField>
        </div>
        <div class="modal-form__row">
          <FormField label="忽略后缀判重">
            <button
              type="button"
              class="drama-switch"
              :class="{ 'drama-switch--on': form.ignore_extension }"
              @click="form.ignore_extension = !form.ignore_extension"
            >
              <span class="drama-switch__dot" />
              <span class="drama-switch__text">{{ form.ignore_extension ? "忽略后缀" : "严格判重" }}</span>
            </button>
          </FormField>
          <FormField label="文件起始（start_fid）">
            <AppInput v-model="form.start_fid" placeholder="可选：只转存修改日期晚于该文件的文件" />
          </FormField>
        </div>

        <div class="drama-form__section">
          <div class="drama-form__section-head">
            <span class="drama-form__section-bar" aria-hidden="true"></span>
            <i class="fas fa-clock-rotate-left drama-form__section-icon"></i>
            <span class="drama-form__section-title">更新与时间</span>
            <span class="drama-form__section-hint">子目录筛选、结束日期与运行星期</span>
          </div>
        </div>
        <div class="modal-form__row">
          <FormField label="需转存的文件夹（update_subdir）">
            <AppInput v-model="form.update_subdir" placeholder="正则，例如：^更新$（留空则处理全部）" />
          </FormField>
          <FormField label="更新目录重存模式">
            <AppSelect
              v-model="form.update_subdir_resave_mode"
              :options="[
                { value: 'none', label: '不重存' },
                { value: 'delete_then_resave', label: '删除后重存' },
              ]"
            />
          </FormField>
        </div>
        <div class="modal-form__row modal-form__row--split">
          <FormField label="截止日期（YYYY-MM-DD）">
            <AppInput v-model="form.end_date" placeholder="例如：2099-12-31" />
          </FormField>
          <FormField label="运行星期">
            <div class="drama-week">
              <div class="drama-week__pills" role="group" aria-label="运行星期">
                <button
                  v-for="w in WEEK_OPTIONS"
                  :key="w.value"
                  type="button"
                  class="drama-week__pill"
                  :class="{ 'drama-week__pill--on': weekSelection.includes(w.value) }"
                  :aria-pressed="weekSelection.includes(w.value)"
                  @click="toggleWeek(w.value)"
                >
                  {{ w.label }}
                </button>
              </div>
              <div class="drama-week__hint">
                <i class="fas fa-circle-info"></i>
                <span>留空表示每天都能跑；勾选后仅在这些周几执行。</span>
              </div>
            </div>
          </FormField>
        </div>
      </div>

      <template #footer>
        <div class="modal-form__footer drama-modal-footer">
          <div class="drama-modal-footer__hint">
            <i class="fas fa-circle-info"></i>
            <span>“保存并运行一次”会在保存后立即触发一次转存，适合首次配置验证。</span>
          </div>
          <AppButton type="button" variant="cancel" @click="drawerOpen = false">取消</AppButton>
          <AppButton type="button" variant="secondary" :disabled="submitting" @click="submitTask">
            <i class="fas fa-floppy-disk"></i>
            {{ submitting ? "保存中…" : "保存" }}
          </AppButton>
          <AppButton type="button" variant="primary" :disabled="submitting" @click="submitAndRunOnce">
            <i class="fas fa-bolt"></i>
            {{ submitting ? "处理中…" : "保存并运行一次" }}
          </AppButton>
        </div>
      </template>
    </AppModal>

    <!-- 目录选择弹窗（来自Trae） -->
    <FolderPickerModal
      :open="pickerOpen"
      selectable-account
      :accounts="activeAccounts"
      :account-id="form.account_id || null"
      :initial-path="form.save_path"
      @close="pickerOpen = false"
      @resolve="onFolderPicked"
    />

    <!-- 运行记录抽屉（来自Trae） -->
    <Teleport to="body">
      <div v-if="runsDrawerOpen" class="runs-drawer-overlay" @click.self="closeRuns">
        <div class="runs-drawer">
          <div class="runs-drawer-head">
            <div>
              <div class="runs-drawer-title">运行记录</div>
              <div class="runs-drawer-sub">{{ runsTask?.task_name }} · 最近 20 条</div>
            </div>
            <button type="button" class="runs-drawer-close" title="关闭" @click="closeRuns">
              <i class="fas fa-times"></i>
            </button>
          </div>
          <div class="runs-drawer-body">
            <div v-if="runsLoading" class="runs-drawer-empty">加载中...</div>
            <div v-else-if="runs.length === 0" class="runs-drawer-empty">暂无运行记录</div>
            <ul v-else class="runs-list">
              <li v-for="run in runs" :key="run.id" class="runs-item" :class="run.status">
                <button type="button" class="runs-card-head" @click="toggleRunExpanded(run.id)">
                  <span class="runs-status-mini" :class="run.status">{{ runStatusText(run.status) }}</span>
                  <i class="fas fa-chevron-down runs-expand-ico" :class="{ open: runExpanded.has(run.id) }"></i>
                  <span class="runs-item-meta-line">
                    {{ formatTime(run.started_at) }}
                    <template v-if="run.transfer_count > 0"> · 转存 {{ run.transfer_count }} 项</template>
                  </span>
                </button>
                <div v-if="runExpanded.has(run.id)" class="runs-detail">
                  <div v-if="run.message" class="runs-detail__msg">{{ run.message }}</div>
                  <pre v-if="run.tree_summary" class="runs-detail__tree">{{ run.tree_summary }}</pre>
                  <div v-if="!run.message && !run.tree_summary" class="runs-detail__empty">无详细日志</div>
                </div>
              </li>
            </ul>
          </div>
        </div>
      </div>
    </Teleport>

    <!-- 分享链接预览弹窗（来自Trae，复刻 CASX 分享目录层级浏览） -->
    <DramaSharePreviewModal
      :open="previewOpen"
      :account-id="form.account_id"
      :share-url="previewParams.shareUrl"
      :task-name="previewParams.taskName"
      :pattern="previewParams.pattern"
      :replace="previewParams.replace"
      :sort-index="previewParams.sortIndex"
      :save-path="previewParams.savePath"
      :ignore-extension="previewParams.ignoreExtension"
      :update-subdir="previewParams.updateSubdir"
      :start-fid="previewParams.startFid"
      @close="previewOpen = false"
      @select="onPreviewSelect"
    />
  </div>
</template>

<style scoped>
.drama-page {
  padding-bottom: 24px;
}

.panel-head-actions {
  display: flex;
  align-items: center;
  gap: 12px;
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 14px 20px;
  border-bottom: 1px solid var(--border-soft);
  background: linear-gradient(180deg, var(--panel-head-bg) 0%, var(--surface) 100%);
}

.panel-head__title {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  flex-shrink: 0;
}

.panel-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-size: 15.5px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: 0.01em;
}

.panel-title__icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: var(--accent-soft);
  color: var(--brand);
  font-size: 12px;
}

.panel-sub {
  color: var(--text-muted);
  font-size: 12.5px;
  line-height: 1.5;
}

.panel-head__toolbar {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 1;
  min-width: 0;
}

.panel-head__divider {
  width: 1px;
  height: 22px;
  background: var(--border);
  opacity: 0.8;
}

.panel-head__meta {
  display: flex;
  align-items: center;
  gap: 10px;
}

.panel-head__badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border-radius: var(--radius-pill);
  background: color-mix(in srgb, var(--brand) 8%, transparent);
  color: var(--brand-strong);
  font-size: 12px;
  font-weight: 600;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  white-space: nowrap;
}

.panel-head__badge-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--brand);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--brand) 20%, transparent);
}

.table-wrap {
  overflow-x: auto;
}

.drama-table {
  min-width: 880px;
  table-layout: fixed;
}

.col-name { width: 22%; }
.col-account { width: 10%; }
.col-path { width: 24%; }
.col-pattern { width: 12%; }
.col-last { width: 15%; }
.col-op { width: 17%; }

.drama-table th.col-op {
  text-align: center;
}

.drama-name {
  color: var(--text);
  font-weight: 600;
  font-size: 14px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.drama-desc {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 4px;
  flex-wrap: wrap;
}

.drama-desc__week {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  color: var(--text-muted);
  font-size: 11.5px;
  padding: 2px 8px;
  border: 1px solid var(--border);
  border-radius: 999px;
  background: var(--surface-sunken);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  letter-spacing: 0.02em;
}

.drama-desc__week i {
  font-size: 10px;
}

.drama-account {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--text-regular);
}

.drama-path {
  color: var(--text-regular);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.drama-pattern {
  display: inline-flex;
  align-items: center;
  max-width: 100%;
  padding: 3px 10px;
  border: 1px solid var(--border);
  border-radius: var(--radius-pill);
  background: color-mix(in srgb, var(--brand) 6%, var(--surface));
  color: var(--accent-text);
  font-size: 11.5px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  line-height: 1.3;
}

.drama-last__status {
  font-weight: 600;
  color: var(--text-regular);
  font-size: 13px;
}

.drama-last__time {
  margin-top: 2px;
  color: var(--text-muted);
  font-size: 11.5px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.drama-filters {
  display: flex;
  align-items: center;
  gap: 8px;
}

.drama-search {
  position: relative;
  display: inline-flex;
  align-items: center;
}

.drama-search__icon {
  position: absolute;
  left: 10px;
  top: 50%;
  transform: translateY(-50%);
  color: var(--text-muted);
  font-size: 12px;
  pointer-events: none;
}

.drama-search__input {
  width: 220px;
  padding: 8px 30px 8px 30px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--surface);
  color: var(--text);
  font-size: 13px;
  transition: border-color var(--transition), box-shadow var(--transition), background var(--transition);
}

.drama-search__input::placeholder {
  color: var(--text-muted);
  opacity: 0.75;
}

.drama-search__input:focus {
  outline: none;
  border-color: var(--brand);
  background: var(--surface);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.drama-search__clear {
  position: absolute;
  right: 6px;
  top: 50%;
  transform: translateY(-50%);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: transparent;
  color: var(--text-muted);
  font-size: 11px;
  cursor: pointer;
  transition: background var(--transition), color var(--transition);
}

.drama-search__clear:hover {
  background: var(--surface-sunken);
  color: var(--text);
}

.drama-filters :deep(.select) {
  width: 132px;
}

/* 表单弹窗（来自Trae） */
.drama-form {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

/* 分区头（来自Trae）：左竖条 + 图标 + 标题 + 描述，视觉分区更清晰 */
.drama-form__section {
  margin-top: 2px;
  margin-bottom: 2px;
}

.drama-form__section-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 0 6px 8px;
  border-bottom: 1px solid var(--border-soft);
}

.drama-form__section-bar {
  display: inline-block;
  width: 3px;
  height: 14px;
  border-radius: 2px;
  background: linear-gradient(180deg, var(--brand-start), var(--brand-end));
  flex-shrink: 0;
}

.drama-form__section-icon {
  color: var(--brand);
  font-size: 12px;
  flex-shrink: 0;
}

.drama-form__section-title {
  font-size: 13.5px;
  font-weight: 700;
  color: var(--text);
  letter-spacing: 0.01em;
}

.drama-form__section-hint {
  margin-left: auto;
  color: var(--text-muted);
  font-size: 11.5px;
  font-weight: 400;
  padding-right: 4px;
}

.share-url-row,
.save-path-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.share-url-row .app-input,
.save-path-row .app-input {
  flex: 1;
  min-width: 0;
}

.share-url-row .app-button,
.save-path-row .app-button {
  flex-shrink: 0;
}

.drama-form__section-title {
  /* kept for backward compat with legacy section title */
}

.modal-form__row--split {
  align-items: stretch;
}

/* 弹窗底部按钮区（来自Trae）：左信息提示 + 右按钮组，主操作与次操作视觉分层 */
.drama-modal-footer {
  align-items: center;
  gap: 10px;
  padding-top: 12px;
  border-top: 1px solid var(--border-soft);
  margin-top: 12px;
}

.drama-modal-footer__hint {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-right: auto;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.4;
}

.drama-modal-footer__hint i {
  color: var(--info);
  font-size: 12px;
  flex-shrink: 0;
}

/* 运行星期 pill 分段按钮（来自Trae）：7 个均分，替代拥挤的原生 checkbox */
.drama-week {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}

.drama-week__pills {
  display: grid;
  grid-template-columns: repeat(7, 1fr);
  gap: 6px;
}

.drama-week__pill {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  height: 32px;
  padding: 0 6px;
  border: 1px solid var(--border);
  border-radius: var(--radius-control);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 12.5px;
  font-weight: 500;
  cursor: pointer;
  transition:
    background var(--transition),
    border-color var(--transition),
    color var(--transition),
    box-shadow var(--transition),
    transform 0.1s ease;
  user-select: none;
}

.drama-week__pill:hover:not(:disabled) {
  border-color: var(--brand);
  color: var(--brand);
}

.drama-week__pill:active:not(:disabled) {
  transform: translateY(1px);
}

.drama-week__pill--on {
  background: var(--brand);
  border-color: var(--brand);
  color: var(--text-on-brand);
  box-shadow: var(--shadow-brand);
  font-weight: 600;
}

.drama-week__pill--on:hover:not(:disabled) {
  color: var(--text-on-brand);
}

.drama-week__pill:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.drama-week__hint {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 6px 10px;
  border-left: 2px solid var(--info);
  background: color-mix(in srgb, var(--info) 8%, transparent);
  color: var(--text-muted);
  font-size: 11.5px;
  line-height: 1.4;
  border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
}

.drama-week__hint i {
  color: var(--info);
  font-size: 12px;
  flex-shrink: 0;
}

.drama-switch {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  cursor: pointer;
}

.drama-switch__dot {
  width: 16px;
  height: 16px;
  border-radius: 50%;
  border: 2px solid var(--text-muted);
  box-sizing: border-box;
}

.drama-switch--on .drama-switch__dot {
  border-color: var(--success);
  background: var(--success);
  box-shadow: inset 0 0 0 2px var(--surface);
}

.drama-switch__text {
  font-size: 13px;
  color: var(--text-regular);
}

/* 运行记录抽屉（来自Trae，风格同 AutomationPanel） */
.runs-drawer-overlay {
  position: fixed;
  inset: 0;
  z-index: 1100;
  background: rgba(15, 23, 42, 0.45);
  display: flex;
  justify-content: flex-end;
}

.runs-drawer {
  width: min(480px, 92vw);
  height: 100%;
  display: flex;
  flex-direction: column;
  background: var(--surface);
  box-shadow: -8px 0 24px rgba(15, 23, 42, 0.18);
}

.runs-drawer-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding: 16px 20px;
  border-bottom: 1px solid var(--border-soft);
  background: var(--panel-head-bg);
}

.runs-drawer-title {
  font-size: 15.5px;
  font-weight: 800;
  color: var(--text);
}

.runs-drawer-sub {
  margin-top: 3px;
  color: var(--text-muted);
  font-size: 12.5px;
}

.runs-drawer-close {
  flex-shrink: 0;
  width: 30px;
  height: 30px;
  border: none;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-muted);
  font-size: 16px;
  cursor: pointer;
}

.runs-drawer-close:hover {
  background: var(--surface-sunken);
  color: var(--text);
}

.runs-drawer-body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 12px 16px;
}

.runs-drawer-empty {
  padding: 40px 0;
  text-align: center;
  color: var(--text-muted);
  font-size: 13px;
}

.runs-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.runs-item {
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-md);
  background: var(--surface);
  overflow: hidden;
}

.runs-card-head {
  width: 100%;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border: none;
  background: transparent;
  cursor: pointer;
  text-align: left;
  color: var(--text);
}

.runs-card-head:hover {
  background: var(--surface-sunken);
}

.runs-status-mini {
  flex-shrink: 0;
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  font-size: 12px;
  font-weight: 600;
}

.runs-status-mini.success {
  background: color-mix(in srgb, var(--success) 12%, transparent);
  color: var(--success);
}

.runs-status-mini.failed {
  background: color-mix(in srgb, var(--danger) 12%, transparent);
  color: var(--danger);
}

.runs-status-mini.running {
  background: color-mix(in srgb, var(--warning) 12%, transparent);
  color: var(--warning);
}

.runs-status-mini.skipped {
  background: var(--surface-sunken);
  color: var(--text-muted);
}

.runs-expand-ico {
  margin-left: auto;
  color: var(--text-muted);
  font-size: 12px;
  transition: transform 0.18s ease;
}

.runs-expand-ico.open {
  transform: rotate(180deg);
}

.runs-item-meta-line {
  color: var(--text-muted);
  font-size: 12px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.runs-detail {
  padding: 10px 12px;
  border-top: 1px solid var(--border-soft);
}

.runs-detail__msg {
  color: var(--text-regular);
  font-size: 13px;
  line-height: 1.6;
}

.runs-detail__tree {
  margin: 8px 0 0;
  padding: 10px;
  background: var(--surface-sunken);
  border-radius: var(--radius-sm);
  color: var(--text-regular);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 300px;
  overflow-y: auto;
}

.runs-detail__empty {
  color: var(--text-muted);
  font-size: 12px;
}

@media (max-width: 720px) {
  .panel-head {
    flex-direction: column;
    align-items: stretch;
  }

  .panel-head-actions {
    flex-wrap: wrap;
  }

  .drama-filters {
    width: 100%;
  }

  .drama-filter-input {
    flex: 1;
    width: auto;
  }
}
</style>
