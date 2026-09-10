<script setup lang="ts">
// 转存设置面板（来自Trae）
// 调度 / 默认命名 / 通知开关全部走后端 settings API，跨浏览器/机器一致（来自Trae）
import { onMounted, ref } from "vue";
import AppInput from "@/components/base/AppInput.vue";
import AppSelect from "@/components/base/AppSelect.vue";
import MagicRegexRules from "@/components/admin/MagicRegexRules.vue";
import SettingsBoolSegment from "@/components/admin/SettingsBoolSegment.vue";
import SettingsCard from "@/components/admin/SettingsCard.vue";
import SettingsRow from "@/components/admin/SettingsRow.vue";
import SettingsHelpTooltip from "@/components/admin/SettingsHelpTooltip.vue";
import { fetchSettings, saveSettings, type SettingItem } from "@/api/settings";
import CronBuilder from "@/components/admin/CronBuilder.vue";
import { bindSettingsPanelExpose, useSettingsForm } from "@/composables/useSettingsForm";
import { toast } from "@/composables/useToast";
import "@/styles/admin-shared.css";

const DRAMA_SETTINGS_ACCENT = "#2563eb";
const DRAMA_NOTIFY_ACCENT = "#f59e0b";
const DRAMA_RULE_ACCENT = "#7c3aed";
const DRAMA_SCHEDULER_ACCENT = "#059669";

// 6 个字段全部持久化到后端 settings 表（来自Trae）
type DramaSettingsForm = {
  scheduler_enabled: boolean;
  scheduler_crontab: string;
  default_pattern: string;
  default_replace: string;
  notify_success: boolean;
  notify_failure: boolean;
};

type FieldKey = keyof DramaSettingsForm;

// 前端字段 → 后端 settings key（来自Trae）
const FIELD_TO_KEY: Record<FieldKey, string> = {
  scheduler_enabled: "drama_scheduler_enabled",
  scheduler_crontab: "drama_scheduler_crontab",
  default_pattern: "drama_default_pattern",
  default_replace: "drama_default_replace",
  notify_success: "drama_notify_success",
  notify_failure: "drama_notify_failure",
};

const ALL_FIELDS = Object.keys(FIELD_TO_KEY) as FieldKey[];
const BOOL_FIELDS: FieldKey[] = ["scheduler_enabled", "notify_success", "notify_failure"];

// 后端未命中该 key 时的兜底默认值，与 registry.go 保持一致（来自Trae）
const FALLBACK_DEFAULTS: Record<string, string> = {
  drama_scheduler_enabled: "true",
  drama_scheduler_crontab: "0 */2 * * *",
  drama_default_pattern: "$TV_REGEX",
  drama_default_replace: "",
  drama_notify_success: "true",
  drama_notify_failure: "true",
};

function toBool(raw: string | undefined, fallback: string): boolean {
  return (raw ?? fallback) === "true";
}

function readRaw(item: SettingItem | undefined, fallback: string): string {
  return item && item.value !== undefined ? item.value : fallback;
}

const {
  settings,
  isDirty,
  isFieldChanged,
  snapshotBaseline,
  revert: revertSettings,
} = useSettingsForm<DramaSettingsForm>({
  scheduler_enabled: true,
  scheduler_crontab: "0 */2 * * *",
  default_pattern: "$TV_REGEX",
  default_replace: "",
  notify_success: true,
  notify_failure: true,
});

const loading = ref(false);
const saving = ref(false);

const patternOptions = [
  { value: "$TV_REGEX", label: "TV 正则（通用剧集）" },
  { value: "$TV_MAGIC", label: "TV 魔法（剧集过滤杂质）" },
  { value: "$SHOW_MAGIC", label: "综艺魔法（过滤杂质）" },
  { value: "$SHOW_PRO", label: "综艺 Pro（过滤杂质）" },
  { value: "$BLACK_WORD", label: "黑名单过滤（剔除广告/预告）" },
];

// 加载后端配置：value 灌 settings，随后对齐 baseline，让"改动"仅反映当前会话内的用户编辑（来自Trae）
async function loadAll() {
  loading.value = true;
  try {
    const payload = await fetchSettings();
    const map: Record<string, SettingItem> = {};
    for (const it of payload.items) map[it.key] = it;
    for (const f of ALL_FIELDS) {
      const key = FIELD_TO_KEY[f];
      const item = map[key];
      const fb = FALLBACK_DEFAULTS[key];
      const raw = readRaw(item, fb);
      const typed = BOOL_FIELDS.includes(f) ? toBool(raw, fb) : raw;
      (settings as Record<string, unknown>)[f] = typed;
    }
    snapshotBaseline();
  } catch {
    // 静默降级：继续使用前端初始默认（来自Trae）
  } finally {
    loading.value = false;
  }
}

async function saveAll() {
  if (!isDirty.value) return;
  saving.value = true;
  try {
    const changed: Record<string, string> = {};
    for (const f of ALL_FIELDS) {
      if (!isFieldChanged(f)) continue;
      const v = settings[f];
      changed[FIELD_TO_KEY[f]] = typeof v === "boolean" ? (v ? "true" : "false") : String(v ?? "");
    }
    if (Object.keys(changed).length === 0) return;
    await saveSettings(changed);
    // 用当前值快照 baseline，避免后端返回不全导致 isDirty 仍为 true（来自Trae）
    snapshotBaseline();
    toast.success("转存设置已保存");
  } catch {
    toast.error("保存失败");
  } finally {
    saving.value = false;
  }
}

function revertAll() {
  revertSettings();
}

onMounted(() => {
  loadAll();
});

defineExpose(
  bindSettingsPanelExpose({
    isDirty,
    saving,
    save: saveAll,
    reload: loadAll,
    revert: revertAll,
  }),
);
</script>

<template>
  <div class="drama-settings">
    <!-- 定时任务配置卡片，来自Trae -->
    <SettingsCard title="定时任务" :accent="DRAMA_SCHEDULER_ACCENT">
      <SettingsRow :show-changed-badge="true" :changed="isFieldChanged('scheduler_enabled')">
        <template #info>
          <div class="settings-row__label">
            <span>启用定时转存</span>
            <SettingsHelpTooltip title="启用定时转存">
              <p>开启后按 Cron 表达式定时扫描所有已启用的转存任务，配合任务级运行星期/截止日期判断是否执行。</p>
              <p>关闭后仅支持手动触发转存。</p>
            </SettingsHelpTooltip>
          </div>
        </template>
        <template #control>
          <SettingsBoolSegment v-model="settings.scheduler_enabled" label="启用定时转存" />
        </template>
      </SettingsRow>

      <SettingsRow :show-changed-badge="true" :changed="isFieldChanged('scheduler_crontab')">
        <template #info>
          <div class="settings-row__label">
            <span>Cron 表达式</span>
            <SettingsHelpTooltip title="Cron 表达式说明">
              <p>标准 5 段式 Cron：<code>分 时 日 月 周</code>。这是全局唯一调度开关，任务级不再单独设置运行星期。</p>
              <p>常用示例：</p>
              <ul>
                <li><code>0 */2 * * *</code> = 每天整点每 2 小时检查一次</li>
                <li><code>0 8 * * *</code> = 每天 8:00 执行</li>
                <li><code>0 8 * * 1-5</code> = 工作日 8:00 执行</li>
                <li><code>0 8 * * 2,4</code> = 每周二/四 8:00 执行</li>
              </ul>
              <p>调度器每 30 秒 tick 一次，只有在 Cron 匹配的分钟才会触发任务扫描。</p>
            </SettingsHelpTooltip>
          </div>
        </template>
        <template #control>
          <CronBuilder v-model="settings.scheduler_crontab" />
        </template>
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="命名默认值" :accent="DRAMA_SETTINGS_ACCENT">
      <SettingsRow :show-changed-badge="true" :changed="isFieldChanged('default_pattern')">
        <template #info>
          <div class="settings-row__label">
            <span>默认命名规则</span>
            <SettingsHelpTooltip title="默认命名规则说明">
              <p>新建转存任务时，默认填入的命名规则键。可在任务详情或命名规则页单独覆盖。</p>
            </SettingsHelpTooltip>
          </div>
        </template>
        <template #control>
          <AppSelect v-model="settings.default_pattern" :options="patternOptions" />
        </template>
      </SettingsRow>

      <SettingsRow :show-changed-badge="true" :changed="isFieldChanged('default_replace')">
        <template #info>
          <div class="settings-row__label">
            <span>默认替换模板</span>
            <SettingsHelpTooltip title="默认替换模板说明">
              <p>与默认命名规则配套的替换模板，留空表示不替换。可在任务详情中单独修改。</p>
            </SettingsHelpTooltip>
          </div>
        </template>
        <template #control>
          <AppInput v-model="settings.default_replace" placeholder="例如 第$1季" />
        </template>
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="通知" :accent="DRAMA_NOTIFY_ACCENT">
      <SettingsRow :show-changed-badge="true" :changed="isFieldChanged('notify_success')">
        <template #info>
          <div class="settings-row__label">
            <span>转存成功通知</span>
            <SettingsHelpTooltip title="转存成功通知说明">
              <p>转存任务成功执行后，是否发送通知。</p>
            </SettingsHelpTooltip>
          </div>
        </template>
        <template #control>
          <SettingsBoolSegment v-model="settings.notify_success" label="转存成功通知" />
        </template>
      </SettingsRow>

      <SettingsRow :show-changed-badge="true" :changed="isFieldChanged('notify_failure')">
        <template #info>
          <div class="settings-row__label">
            <span>转存失败通知</span>
            <SettingsHelpTooltip title="转存失败通知说明">
              <p>转存任务执行失败后，是否发送通知。</p>
            </SettingsHelpTooltip>
          </div>
        </template>
        <template #control>
          <SettingsBoolSegment v-model="settings.notify_failure" label="转存失败通知" />
        </template>
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="命名规则" :accent="DRAMA_RULE_ACCENT">
      <MagicRegexRules />
    </SettingsCard>
  </div>
</template>
