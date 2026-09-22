<script setup lang="ts">
// 转存任务仪表带（来自Trae）：与 STRM / 整理 页共用 SignalBand 外壳，
// 中列展示最近执行结果，右侧「转存设置」入口直连 DramaSettingsPanel 抽屉。
import { computed, onMounted, reactive } from "vue";
import { fetchSettings } from "@/api/settings";
import type { DramaTask } from "@/api/drama";
import BandMidRows from "@/components/admin/band/BandMidRows.vue";
import SignalBand from "@/components/admin/band/SignalBand.vue";

const props = withDefaults(
  defineProps<{
    tasks?: DramaTask[];
    refreshPending?: boolean;
  }>(),
  {
    tasks: () => [],
    refreshPending: false,
  },
);

const emit = defineEmits<{ refresh: []; "open-settings": []; dismiss: [event?: MouseEvent] }>();

// 任务状态：以 DramaTask.status 为准（来自Trae）
const total = computed(() => props.tasks.length);
const enabledCount = computed(() => props.tasks.filter((t) => t.status === "running").length);
const errorCount = computed(() => props.tasks.filter((t) => t.status === "error").length);
const pausedCount = computed(() => Math.max(0, total.value - enabledCount.value - errorCount.value));

// 最近一次执行是否成功（来自Trae，取最新 run_status === 'success' 的任务占比）
const successRate = computed<number | null>(() => {
  if (!props.tasks.length) return null;
  const withRuns = props.tasks.filter((t) => t.last_run_at);
  if (!withRuns.length) return null;
  const ok = withRuns.filter((t) => t.last_run_status === "success").length;
  return Math.round((ok / withRuns.length) * 100);
});

const statRows = computed(() => [
  { key: "total", label: "任务总数", value: total.value, tone: "brand" as const },
  { key: "enabled", label: "已启用", value: enabledCount.value, tone: "success" as const },
  { key: "error", label: "异常", value: errorCount.value, tone: "warn" as const },
]);

// 状态条：启用 / 异常 / 已停用（来自Trae）
const barSegments = computed(() => [
  { key: "enabled", label: "已启用", value: enabledCount.value, tone: "success" as const },
  { key: "error", label: "异常", value: errorCount.value, tone: "warn" as const },
  { key: "paused", label: "已停用", value: pausedCount.value, tone: "muted" as const },
]);

// 最近一次执行结果（来自Trae，扫描所有任务中最新一次的 last_run_at）
const latestRun = computed(() => {
  const withRuns = props.tasks
    .filter((t) => t.last_run_at)
    .sort((a, b) => new Date(b.last_run_at ?? 0).getTime() - new Date(a.last_run_at ?? 0).getTime());
  return withRuns[0] ?? null;
});

const withRunCount = computed(() => props.tasks.filter((t) => t.last_run_at).length);

function lastRunLabel(status?: string): string {
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
      return "未执行";
  }
}

const outputRows = computed(() => {
  const run = latestRun.value;
  if (!run) {
    return [
      { key: "lastRun", label: "最近一次执行", value: "尚未执行" },
      { key: "cover", label: "覆盖任务", value: `${withRunCount.value} / ${total.value}` },
    ];
  }
  return [
    {
      key: "lastRun",
      label: `最近一次 · ${run.task_name}`,
      value: `${lastRunLabel(run.last_run_status)} · ${formatTime(run.last_run_at)}`,
    },
    { key: "cover", label: "有执行记录", value: `${withRunCount.value} / ${total.value}` },
  ];
});

function formatTime(iso?: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

// 设置摘要：与 STRM 页一样，从后端 settings 拉一次展示到右侧「转存设置」卡（来自Trae）
const settings = reactive({
  scheduler_enabled: true,
  crontab: "*/30 * * * *",
  default_pattern: "$TV_REGEX",
  notify_success: true,
  notify_failure: true,
});
const settingsLoading = reactive({ value: true });

const setupRows = computed(() => {
  const loading = settingsLoading.value;
  return [
    {
      key: "cron",
      label: "扫描频率",
      value: loading ? "读取中…" : settings.scheduler_enabled ? settings.crontab : "已关闭",
    },
    {
      key: "rule",
      label: "默认规则",
      value: loading ? "读取中…" : settings.default_pattern || "—",
    },
    {
      key: "notify",
      label: "通知",
      value: loading
        ? "读取中…"
        : settings.notify_success && settings.notify_failure
          ? "成功 / 失败"
          : settings.notify_success
            ? "仅成功"
            : settings.notify_failure
              ? "仅失败"
              : "关闭",
    },
  ];
});

const DRAMA_KEYS = [
  "drama_scheduler_enabled",
  "drama_scheduler_crontab",
  "drama_default_pattern",
  "drama_notify_success",
  "drama_notify_failure",
] as const;

async function reloadSettings() {
  settingsLoading.value = true;
  try {
    const payload = await fetchSettings();
    const map: Record<string, string> = {};
    for (const it of payload.items) {
      if ((DRAMA_KEYS as readonly string[]).includes(it.key)) map[it.key] = it.value ?? "";
    }
    settings.scheduler_enabled = map.drama_scheduler_enabled !== "false";
    settings.crontab = map.drama_scheduler_crontab || "*/30 * * * *";
    settings.default_pattern = map.drama_default_pattern || "$TV_REGEX";
    settings.notify_success = map.drama_notify_success !== "false";
    settings.notify_failure = map.drama_notify_failure !== "false";
  } catch {
    // 静默降级：继续使用默认摘要（来自Trae）
  } finally {
    settingsLoading.value = false;
  }
}

onMounted(() => {
  void reloadSettings();
});

defineExpose({ reloadSettings });
</script>

<template>
  <SignalBand
    :ring-percent="successRate"
    ring-label="执行成功率"
    :stats="statRows"
    bar-label="任务状态"
    :bar-segments="barSegments"
    setup-title="转存设置"
    :setup-rows="setupRows"
    @open-settings="emit('open-settings')"
    @dismiss="emit('dismiss', $event)"
  >
    <template #middle>
      <BandMidRows :rows="outputRows">
        <template #cover>
          <button
            type="button"
            class="mid-act"
            :disabled="refreshPending"
            :title="refreshPending ? '刷新中' : '刷新转存任务列表'"
            aria-label="刷新转存任务列表"
            @click="emit('refresh')"
          >
            <svg viewBox="0 0 24 24" width="14" height="14" aria-hidden="true">
              <path
                d="M20 12a8 8 0 1 1-2.34-5.66"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
              />
              <path d="M20 4v4h-4" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </button>
        </template>
      </BandMidRows>
    </template>
  </SignalBand>
</template>

<style scoped>
.mid-act {
  display: inline-grid;
  place-items: center;
  width: 26px;
  height: 26px;
  border: 1px solid transparent;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-muted);
  cursor: pointer;
  transition: color 0.16s ease, border-color 0.16s ease, background 0.16s ease;
}

.mid-act:hover:not(:disabled) {
  color: var(--brand);
  border-color: var(--border);
  background: var(--surface-hover);
}

.mid-act:focus-visible {
  outline: 2px solid color-mix(in srgb, var(--brand) 40%, transparent);
  outline-offset: 1px;
}

.mid-act:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
