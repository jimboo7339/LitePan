<script setup lang="ts">
// 转存设置面板（来自Trae）
// 包含：定时任务配置（全局 settings API）+ 命名默认值（localStorage）+ 通知（localStorage）+ 命名规则
import { computed, onMounted, ref } from "vue";
import AppInput from "@/components/base/AppInput.vue";
import AppSelect from "@/components/base/AppSelect.vue";
import MagicRegexRules from "@/components/admin/MagicRegexRules.vue";
import SettingsBoolSegment from "@/components/admin/SettingsBoolSegment.vue";
import SettingsCard from "@/components/admin/SettingsCard.vue";
import SettingsRow from "@/components/admin/SettingsRow.vue";
import SettingsHelpTooltip from "@/components/admin/SettingsHelpTooltip.vue";
import { fetchSettings, saveSettings, type SettingItem } from "@/api/settings";
import { bindSettingsPanelExpose, useSettingsForm } from "@/composables/useSettingsForm";
import { toast } from "@/composables/useToast";
import "@/styles/admin-shared.css";

const DRAMA_SETTINGS_ACCENT = "#2563eb";
const DRAMA_NOTIFY_ACCENT = "#f59e0b";
const DRAMA_RULE_ACCENT = "#7c3aed";
const DRAMA_SCHEDULER_ACCENT = "#059669";

// === 定时任务配置（全局 settings API），来自Trae ===
const schedulerLoading = ref(false);
const schedulerSaving = ref(false);
const schedulerEnabled = ref(true);
const schedulerCrontab = ref("0 */2 * * *");
const schedulerDefaults = { enabled: true, crontab: "0 */2 * * *" };

// === 命名默认值 + 通知（localStorage），来自Trae ===
type DramaSettingsForm = {
  default_pattern: string;
  default_replace: string;
  notify_success: boolean;
  notify_failure: boolean;
};

const { settings, isDirty: settingsChanged, revert: revertSettings, snapshotBaseline } = useSettingsForm<DramaSettingsForm>({
  default_pattern: "$TV_REGEX",
  default_replace: "",
  notify_success: true,
  notify_failure: true,
});

const saving = ref(false);

const patternOptions = [
  { value: "$TV_REGEX", label: "TV 正则（通用剧集）" },
  { value: "$TV_MAGIC", label: "TV 魔法（剧集过滤杂质）" },
  { value: "$SHOW_MAGIC", label: "综艺魔法（过滤杂质）" },
  { value: "$SHOW_PRO", label: "综艺 Pro（过滤杂质）" },
  { value: "$BLACK_WORD", label: "黑名单过滤（剔除广告/预告）" },
];

// 脏状态：定时任务配置或本地设置有任一改动即为脏，来自Trae
const isDirty = computed(() => {
  if (schedulerEnabled.value !== schedulerDefaults.enabled) return true;
  if (schedulerCrontab.value !== schedulerDefaults.crontab) return true;
  return settingsChanged.value;
});

async function loadSchedulerSettings() {
  schedulerLoading.value = true;
  try {
    const payload = await fetchSettings();
    const findItem = (key: string): SettingItem | undefined =>
      payload.items.find((it) => it.key === key);
    const enabledItem = findItem("drama_scheduler_enabled");
    const crontabItem = findItem("drama_scheduler_crontab");
    if (enabledItem) {
      schedulerEnabled.value = enabledItem.value === "true";
      schedulerDefaults.enabled = enabledItem.default === "true";
    }
    if (crontabItem) {
      schedulerCrontab.value = crontabItem.value || crontabItem.default || "0 */2 * * *";
      schedulerDefaults.crontab = crontabItem.default || "0 */2 * * *";
    }
  } catch {
    // 静默降级，使用默认值
  } finally {
    schedulerLoading.value = false;
  }
}

function loadLocalSettings() {
  try {
    const raw = localStorage.getItem("litepan:drama:settings");
    if (raw) {
      const data = JSON.parse(raw) as DramaSettingsForm;
      settings.default_pattern = data.default_pattern ?? "$TV_REGEX";
      settings.default_replace = data.default_replace ?? "";
      settings.notify_success = !!data.notify_success;
      settings.notify_failure = !!data.notify_failure;
    }
  } catch {
    // ignore
  }
}

function persistLocalSettings() {
  localStorage.setItem("litepan:drama:settings", JSON.stringify({ ...settings }));
}

async function saveAll() {
  if (!isDirty.value) return;
  saving.value = true;
  schedulerSaving.value = true;
  try {
    // 保存全局调度配置到后端，来自Trae
    const changed: Record<string, string> = {};
    if (schedulerEnabled.value !== schedulerDefaults.enabled) {
      changed["drama_scheduler_enabled"] = schedulerEnabled.value ? "true" : "false";
    }
    if (schedulerCrontab.value !== schedulerDefaults.crontab) {
      changed["drama_scheduler_crontab"] = schedulerCrontab.value;
    }
    if (Object.keys(changed).length > 0) {
      await saveSettings(changed);
      // 直接用当前值更新基线，不依赖 payload 返回值（来自Trae）
      // 之前用 payload.items.find 判断是否更新基线，但后端返回的 items
      // 可能不含我们改的 key，导致基线不更新、isDirty 永远为 true。
      if ("drama_scheduler_enabled" in changed) {
        schedulerDefaults.enabled = schedulerEnabled.value;
      }
      if ("drama_scheduler_crontab" in changed) {
        schedulerDefaults.crontab = schedulerCrontab.value;
      }
    }
    // 保存本地配置，来自Trae
    persistLocalSettings();
    // 重置 useSettingsForm 的 dirty 基线，否则 settingsChanged 永远为 true（来自Trae）
    snapshotBaseline();
    toast.success("转存设置已保存");
  } catch {
    toast.error("保存失败");
  } finally {
    saving.value = false;
    schedulerSaving.value = false;
  }
}

function reloadAll() {
  loadSchedulerSettings();
  loadLocalSettings();
}

function revertAll() {
  schedulerEnabled.value = schedulerDefaults.enabled;
  schedulerCrontab.value = schedulerDefaults.crontab;
  revertSettings();
}

onMounted(() => {
  reloadAll();
});

defineExpose(
  bindSettingsPanelExpose({
    isDirty,
    saving,
    save: saveAll,
    reload: reloadAll,
    revert: revertAll,
  }),
);
</script>

<template>
  <div class="drama-settings">
    <!-- 定时任务配置卡片，来自Trae -->
    <SettingsCard title="定时任务" :accent="DRAMA_SCHEDULER_ACCENT">
      <SettingsRow :show-changed-badge="true" :changed="schedulerEnabled !== schedulerDefaults.enabled">
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
          <SettingsBoolSegment v-model="schedulerEnabled" label="启用定时转存" />
        </template>
      </SettingsRow>

      <SettingsRow :show-changed-badge="true" :changed="schedulerCrontab !== schedulerDefaults.crontab">
        <template #info>
          <div class="settings-row__label">
            <span>Cron 表达式</span>
            <SettingsHelpTooltip title="Cron 表达式说明">
              <p>标准 5 段式 Cron：<code>分 时 日 月 周</code></p>
              <p>常用示例：</p>
              <ul>
                <li><code>0 */2 * * *</code> = 每 2 小时检查一次</li>
                <li><code>0 8 * * *</code> = 每天 8:00 执行</li>
                <li><code>0 8,20 * * *</code> = 每天 8:00 和 20:00 各执行一次</li>
                <li><code>*/30 * * * *</code> = 每 30 分钟检查一次</li>
              </ul>
              <p>调度器每 30 秒 tick 一次，只有在 Cron 匹配的分钟才会触发任务扫描。</p>
            </SettingsHelpTooltip>
          </div>
        </template>
        <template #control>
          <AppInput v-model="schedulerCrontab" placeholder="0 */2 * * *" />
        </template>
      </SettingsRow>
    </SettingsCard>

    <SettingsCard title="命名默认值" :accent="DRAMA_SETTINGS_ACCENT">
      <SettingsRow :show-changed-badge="true" :changed="settings.default_pattern !== '$TV_REGEX'">
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

      <SettingsRow :show-changed-badge="true" :changed="!!settings.default_replace">
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
      <SettingsRow :show-changed-badge="true" :changed="!settings.notify_success">
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

      <SettingsRow :show-changed-badge="true" :changed="!settings.notify_failure">
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
