<script setup lang="ts">
// Cron 表达式可视化构建器（来自Trae）
// 用户不必去在线网站配置后复制，直接选预设 / 勾星期 / 改时点即可实时生成表达式
import { computed, ref, watch } from "vue";
import AppInput from "@/components/base/AppInput.vue";

// 常见预设，覆盖 80% 日常使用场景（来自Trae）
const PRESETS = [
  { label: "每 30 分钟一次", value: "*/30 * * * *" },
  { label: "每 1 小时一次", value: "0 */1 * * *" },
  { label: "每 2 小时一次", value: "0 */2 * * *" },
  { label: "每 6 小时一次", value: "0 */6 * * *" },
  { label: "每天 08:00 执行", value: "0 8 * * *" },
  { label: "每天 08:00 和 20:00 各一次", value: "0 8,20 * * *" },
  { label: "工作日 08:00（周一到周五）", value: "0 8 * * 1-5" },
  { label: "周末 08:00（周六周日）", value: "0 8 * * 6,7" },
  { label: "每周一 08:00 执行", value: "0 8 * * 1" },
  { label: "每天 00:00 执行", value: "0 0 * * *" },
];

// 星期选项：ISO 语义 1=周一…7=周日（来自Trae）
const WEEK_OPTIONS = [
  { value: 1, label: "周一" },
  { value: 2, label: "周二" },
  { value: 3, label: "周三" },
  { value: 4, label: "周四" },
  { value: 5, label: "周五" },
  { value: 6, label: "周六" },
  { value: 7, label: "周日" },
];

const model = defineModel<string>({ required: true, default: "0 */2 * * *" });

// 三种模式：直接展示预设原文；"每天 HH:MM"；"每天 HH:MM 只在勾选的星期"（来自Trae）
type Mode = "preset" | "daily" | "weekly";
const mode = ref<Mode>("preset");
const presetIndex = ref<number>(-1);
const hour = ref<number>(8);
const minute = ref<number>(0);
const weekDays = ref<number[]>([]);

function parseExpression(expr: string) {
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) {
    mode.value = "preset";
    presetIndex.value = PRESETS.findIndex((p) => p.value === expr);
    return;
  }

  // 反查是否命中某个预设：命中则原样展示，避免被 UI 二次改写
  const presetHit = PRESETS.findIndex((p) => p.value === expr);
  if (presetHit >= 0) {
    mode.value = "preset";
    presetIndex.value = presetHit;
    // 顺便把时点、星期显示同步一下，让 UI 看起来合理（来自Trae）
    syncFromExpression(expr);
    return;
  }

  // 非预设：拆解分钟/时/星期字段，选合适模式（来自Trae）
  syncFromExpression(expr);
  if (weekDays.value.length > 0) {
    mode.value = "weekly";
  } else {
    mode.value = "daily";
  }
  presetIndex.value = -1;
}

function syncFromExpression(expr: string) {
  const parts = expr.trim().split(/\s+/);
  if (parts.length !== 5) return;
  const [min, hr, , , week] = parts;
  const m = parseInt(min, 10);
  const h = parseInt(hr, 10);
  if (!Number.isNaN(m) && !min.includes("*") && !min.includes(",") && !min.includes("/") && !min.includes("-")) {
    minute.value = m;
  }
  if (!Number.isNaN(h) && !hr.includes("*") && !hr.includes(",") && !hr.includes("/") && !hr.includes("-")) {
    hour.value = h;
  }
  const days: number[] = [];
  if (week === "*" || week === "") {
    weekDays.value = [];
    return;
  }
  for (const tok of week.split(",")) {
    const t = tok.trim();
    if (!t) continue;
    if (t.includes("-")) {
      const [a, b] = t.split("-").map((n) => parseInt(n, 10));
      if (!Number.isNaN(a) && !Number.isNaN(b)) {
        const lo = Math.min(a, b);
        const hi = Math.max(a, b);
        for (let i = lo; i <= hi; i++) days.push(i);
      }
    } else {
      const n = parseInt(t, 10);
      if (!Number.isNaN(n)) days.push(n);
    }
  }
  weekDays.value = [...new Set(days)].sort((a, b) => a - b);
}

// 生成最终表达式（来自Trae）
const computedExpr = computed(() => {
  if (mode.value === "preset") {
    const p = PRESETS[presetIndex.value];
    return p ? p.value : "0 */2 * * *";
  }
  if (mode.value === "weekly") {
    const wf = weekDays.value.join(",");
    return `${minute.value} ${hour.value} * * ${wf}`;
  }
  return `${minute.value} ${hour.value} * * *`;
});

// 外部写入 model 时解析进内部状态（来自Trae）
watch(
  () => model.value,
  (v) => {
    if (v === computedExpr.value) return;
    parseExpression(v ?? "");
  },
  { immediate: true },
);

// 内部状态变化时写回 model（来自Trae）
watch(computedExpr, (v) => {
  if (v !== model.value) model.value = v;
});

function onPresetChange(idx: number) {
  presetIndex.value = idx;
  mode.value = "preset";
  const p = PRESETS[idx];
  if (p) syncFromExpression(p.value);
}

function toggleWeekDay(day: number) {
  const cur = weekDays.value;
  const next = cur.includes(day) ? cur.filter((d) => d !== day) : [...cur, day].sort((a, b) => a - b);
  weekDays.value = next;
  mode.value = next.length > 0 ? "weekly" : "daily";
  presetIndex.value = -1;
}

function onHourChange(raw: string | number | boolean) {
  const n = Number(raw);
  if (!Number.isNaN(n) && n >= 0 && n <= 23) {
    hour.value = n;
    mode.value = weekDays.value.length > 0 ? "weekly" : "daily";
    presetIndex.value = -1;
  }
}

function onMinuteChange(raw: string | number | boolean) {
  const n = Number(raw);
  if (!Number.isNaN(n) && n >= 0 && n <= 59) {
    minute.value = n;
    mode.value = weekDays.value.length > 0 ? "weekly" : "daily";
    presetIndex.value = -1;
  }
}
</script>

<template>
  <div class="cron-builder">
    <!-- 预设选择（来自Trae） -->
    <div class="cron-builder__row">
      <span class="cron-builder__label">预设</span>
      <select
        class="cron-builder__preset"
        :value="presetIndex"
        @change="onPresetChange(Number(($event.target as HTMLSelectElement).value))"
      >
        <option value="-1" disabled>选择常见频率…</option>
        <option v-for="(p, i) in PRESETS" :key="p.value" :value="i">
          {{ p.label }}（<code>{{ p.value }}</code>）
        </option>
      </select>
    </div>

    <!-- 自定义时点（来自Trae） -->
    <div class="cron-builder__row">
      <span class="cron-builder__label">时点</span>
      <div class="cron-builder__time">
        <AppInput
          :model-value="String(hour).padStart(2, '0')"
          type="number"
          min="0"
          max="23"
          placeholder="时"
          style="width: 72px"
          @update:model-value="onHourChange"
        />
        <span class="cron-builder__colon">:</span>
        <AppInput
          :model-value="String(minute).padStart(2, '0')"
          type="number"
          min="0"
          max="59"
          placeholder="分"
          style="width: 72px"
          @update:model-value="onMinuteChange"
        />
      </div>
    </div>

    <!-- 星期多选（来自Trae） -->
    <div class="cron-builder__row">
      <span class="cron-builder__label">星期</span>
      <div class="cron-builder__week">
        <label
          v-for="w in WEEK_OPTIONS"
          :key="w.value"
          class="cron-builder__week-item"
          :class="{ 'is-on': weekDays.includes(w.value) }"
        >
          <input type="checkbox" :checked="weekDays.includes(w.value)" @change="toggleWeekDay(w.value)" />
          <span>{{ w.label }}</span>
        </label>
        <span class="cron-builder__week-hint" v-if="weekDays.length === 0">不选 = 每天</span>
      </div>
    </div>

    <!-- 结果展示（来自Trae） -->
    <div class="cron-builder__result">
      <span class="cron-builder__label">表达式</span>
      <code class="cron-builder__expr">{{ computedExpr }}</code>
    </div>
  </div>
</template>

<style scoped>
.cron-builder {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px;
  background: var(--lp-fill-2, rgba(15, 23, 42, 0.03));
  border: 1px solid var(--lp-border, #e5e7eb);
  border-radius: 10px;
  font-size: 13px;
}
.cron-builder__row {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.cron-builder__label {
  color: var(--lp-text-muted, #6b7280);
  min-width: 48px;
  flex-shrink: 0;
}
.cron-builder__preset {
  flex: 1;
  min-width: 240px;
  padding: 6px 8px;
  border: 1px solid var(--lp-border, #e5e7eb);
  border-radius: 8px;
  background: var(--lp-bg, #fff);
  color: var(--lp-text, #0f172a);
  font-size: 13px;
}
.cron-builder__preset code {
  color: var(--lp-text-muted, #6b7280);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
.cron-builder__time {
  display: flex;
  align-items: center;
  gap: 6px;
}
.cron-builder__colon {
  color: var(--lp-text-muted, #6b7280);
  font-weight: 600;
}
.cron-builder__week {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  flex: 1;
}
.cron-builder__week-item {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  padding: 3px 8px;
  border: 1px solid var(--lp-border, #e5e7eb);
  border-radius: 999px;
  background: var(--lp-bg, #fff);
  color: var(--lp-text-muted, #6b7280);
  cursor: pointer;
  user-select: none;
  font-size: 12px;
  transition: all 0.15s;
}
.cron-builder__week-item:hover {
  border-color: var(--lp-accent, #2563eb);
  color: var(--lp-accent, #2563eb);
}
.cron-builder__week-item input {
  display: none;
}
.cron-builder__week-item.is-on {
  background: var(--lp-accent, #2563eb);
  border-color: var(--lp-accent, #2563eb);
  color: #fff;
}
.cron-builder__week-hint {
  color: var(--lp-text-muted, #94a3b8);
  font-size: 11px;
  margin-left: 4px;
}
.cron-builder__result {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-top: 6px;
  border-top: 1px dashed var(--lp-border, #e5e7eb);
}
.cron-builder__expr {
  padding: 5px 10px;
  background: var(--lp-fill-1, #f1f5f9);
  border-radius: 6px;
  color: var(--lp-accent, #2563eb);
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  font-weight: 600;
}
</style>
