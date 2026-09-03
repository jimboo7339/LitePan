<script setup lang="ts">
// 命名正则规则维护面板（来自Trae）。
// 复刻 CASX MagicRegexView 的「保存规则」页签：内置规则常驻、可覆盖；
// 自定义规则可新增/启用/删除。改动即写库，转存执行与预览实时生效。
import { computed, onMounted, reactive, ref } from "vue";
import { getApiErrorMessage } from "@/api/client";
import {
  deleteMagicRegexRule,
  fetchMagicRegexRules,
  saveMagicRegexRule,
  type MagicRegexRule,
} from "@/api/drama";
import AppButton from "@/components/base/AppButton.vue";
import AppBadge from "@/components/base/AppBadge.vue";
import AppInput from "@/components/base/AppInput.vue";
import AppModal from "@/components/base/AppModal.vue";
import FormField from "@/components/base/FormField.vue";
import AdminEnableToggle from "@/components/admin/AdminEnableToggle.vue";
import { confirm } from "@/composables/useConfirm";
import { toast } from "@/composables/useToast";
import "@/styles/admin-shared.css";
import "@/styles/admin-table.css";

const loading = ref(false);
const rules = ref<MagicRegexRule[]>([]);

const builtinRules = computed(() => rules.value.filter((r) => r.built_in));
const customRules = computed(() => rules.value.filter((r) => !r.built_in));

// 内置规则 key 集合，用于判定编辑弹窗是否展示「启用」开关（来自Trae）
const builtinKeySet = computed(() => new Set(rules.value.filter((r) => r.built_in).map((r) => r.key)));

// 编辑/新增弹窗状态（来自Trae）
const dialog = reactive({
  visible: false,
  submitting: false,
  isEdit: false,
  keyLocked: false,
  form: {
    key: "",
    label: "",
    enabled: true,
    pattern: "",
    replace: "",
  },
});

function normalizeKey(key: string) {
  return String(key || "").trim();
}

function isValidKey(key: string) {
  const value = normalizeKey(key);
  return value.startsWith("$") && !value.includes(" ") && value.length <= 64;
}

async function refresh() {
  loading.value = true;
  try {
    const data = await fetchMagicRegexRules();
    rules.value = data.rules || [];
  } catch (e) {
    toast.error(getApiErrorMessage(e, "加载规则失败"));
  } finally {
    loading.value = false;
  }
}

function openCreate() {
  dialog.visible = true;
  dialog.submitting = false;
  dialog.isEdit = false;
  dialog.keyLocked = false;
  dialog.form.key = "$";
  dialog.form.label = "";
  dialog.form.enabled = true;
  dialog.form.pattern = "";
  dialog.form.replace = "";
}

function openEdit(rule: MagicRegexRule) {
  dialog.visible = true;
  dialog.submitting = false;
  dialog.isEdit = true;
  dialog.keyLocked = true;
  dialog.form.key = rule.key;
  dialog.form.label = rule.label || "";
  dialog.form.enabled = Boolean(rule.enabled);
  dialog.form.pattern = rule.pattern || "";
  dialog.form.replace = rule.replace || "";
}

async function submit() {
  const key = normalizeKey(dialog.form.key);
  if (!isValidKey(key)) {
    toast.warning("key 必须以 $ 开头，且不能包含空格（最长 64）");
    return;
  }
  if (!dialog.isEdit && !String(dialog.form.pattern || "").trim()) {
    toast.warning("新增规则时 pattern 不能为空");
    return;
  }
  dialog.submitting = true;
  try {
    const data = await saveMagicRegexRule(key, {
      label: dialog.form.label ? String(dialog.form.label).trim() : null,
      enabled: Boolean(dialog.form.enabled),
      pattern: String(dialog.form.pattern || "").trim() || null,
      replace: String(dialog.form.replace || ""),
    });
    rules.value = data.rules || [];
    dialog.visible = false;
    toast.success("已保存");
  } catch (e) {
    toast.error(getApiErrorMessage(e, "保存失败"));
  } finally {
    dialog.submitting = false;
  }
}

// 自定义规则启用切换（内置规则无需切换，覆盖即生效）（来自Trae）
async function toggleCustom(rule: MagicRegexRule, value: boolean) {
  if (rule.built_in) return;
  const key = normalizeKey(rule.key);
  try {
    const data = await saveMagicRegexRule(key, { enabled: Boolean(value) });
    rules.value = data.rules || [];
    toast.success(value ? "已启用" : "已停用");
  } catch (e) {
    toast.error(getApiErrorMessage(e, "更新失败"));
    await refresh();
  }
}

async function removeRule(rule: MagicRegexRule) {
  const title = rule.built_in ? "恢复默认规则" : "删除自定义规则";
  const message = rule.built_in
    ? `将清除对 ${rule.key} 的覆盖配置，恢复为系统默认值。`
    : `将删除 ${rule.key} 规则。`;
  const ok = await confirm({ title, message, confirmText: "确认", danger: !rule.built_in });
  if (!ok) return;
  try {
    const data = await deleteMagicRegexRule(rule.key);
    rules.value = data.rules || [];
    toast.success("已更新");
  } catch (e) {
    toast.error(getApiErrorMessage(e, "操作失败"));
  }
}

onMounted(refresh);
</script>

<template>
  <div class="rules-page">
    <div class="rules-page__hint">
      <div>新增的规则 key 需要以 $ 开头（例如：$MY_RULE）。在追剧任务里将 pattern 设置为该 key，即可使用系统保存规则。</div>
      <div>replace 为默认模板；任务里 replace 留空时，会自动使用该默认值。</div>
    </div>

    <section class="admin-panel-table-wrap rules-card">
      <div class="panel-head">
        <div>
          <div class="panel-title">内置规则</div>
          <div class="panel-sub">常驻规则，可编辑覆盖；「恢复默认」清除覆盖并回退内置值。</div>
        </div>
        <div class="panel-head-actions">
          <AppBadge tone="info">{{ builtinRules.length }} 条</AppBadge>
          <AppButton type="button" size="sm" variant="secondary" :disabled="loading" @click="refresh">
            <i class="fas fa-sync"></i>
            刷新
          </AppButton>
        </div>
      </div>
      <div class="table-wrap">
        <table class="admin-table rules-table">
          <thead>
            <tr>
              <th class="rules-col-key">key</th>
              <th class="rules-col-label">名称</th>
              <th class="rules-col-status">状态</th>
              <th class="rules-col-pattern">pattern</th>
              <th class="rules-col-replace">replace</th>
              <th class="rules-col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading && builtinRules.length === 0">
              <td colspan="6" class="empty-cell">加载中...</td>
            </tr>
            <tr v-else-if="builtinRules.length === 0">
              <td colspan="6" class="empty-cell">暂无内置规则</td>
            </tr>
            <tr v-for="rule in builtinRules" v-else :key="rule.key">
              <td><span class="rules-key">{{ rule.key }}</span></td>
              <td class="rules-label">{{ rule.label || "-" }}</td>
              <td>
                <AppBadge :tone="rule.overridden ? 'warning' : 'info'">
                  {{ rule.overridden ? "已覆盖" : "默认" }}
                </AppBadge>
              </td>
              <td class="rules-mono" :title="rule.pattern">{{ rule.pattern || "-" }}</td>
              <td class="rules-mono" :title="rule.replace">{{ rule.replace || "-" }}</td>
              <td class="admin-table__actions">
                <button type="button" class="rules-btn" @click="openEdit(rule)">编辑</button>
                <button
                  type="button"
                  class="rules-btn"
                  :disabled="!rule.overridden"
                  @click="removeRule(rule)"
                >
                  恢复默认
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="admin-panel-table-wrap rules-card">
      <div class="panel-head">
        <div>
          <div class="panel-title">自定义规则</div>
          <div class="panel-sub">按需新增的命名规则；停用后不再参与转存与预览。</div>
        </div>
        <div class="panel-head-actions">
          <AppBadge tone="info">{{ customRules.length }} 条</AppBadge>
          <AppButton type="button" size="sm" variant="primary" @click="openCreate">
            <i class="fas fa-plus"></i>
            新增规则
          </AppButton>
        </div>
      </div>
      <div class="table-wrap">
        <table class="admin-table rules-table">
          <thead>
            <tr>
              <th class="rules-col-key">key</th>
              <th class="rules-col-label">名称</th>
              <th class="rules-col-status">启用</th>
              <th class="rules-col-pattern">pattern</th>
              <th class="rules-col-replace">replace</th>
              <th class="rules-col-op">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading && customRules.length === 0">
              <td colspan="6" class="empty-cell">加载中...</td>
            </tr>
            <tr v-else-if="customRules.length === 0">
              <td colspan="6" class="empty-cell">还没有自定义规则，点右上角「新增规则」创建</td>
            </tr>
            <tr v-for="rule in customRules" v-else :key="rule.key">
              <td><span class="rules-key">{{ rule.key }}</span></td>
              <td class="rules-label">{{ rule.label || "-" }}</td>
              <td>
                <AdminEnableToggle
                  :enabled="rule.enabled"
                  aria-label="规则启用切换"
                  :on-label="'启'"
                  :off-label="'禁'"
                  @enable="(v) => toggleCustom(rule, v)"
                />
              </td>
              <td class="rules-mono" :title="rule.pattern">{{ rule.pattern || "-" }}</td>
              <td class="rules-mono" :title="rule.replace">{{ rule.replace || "-" }}</td>
              <td class="admin-table__actions">
                <button type="button" class="rules-btn" @click="openEdit(rule)">编辑</button>
                <button type="button" class="rules-btn rules-btn--danger" @click="removeRule(rule)">删除</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <!-- 新增/编辑规则弹窗（来自Trae） -->
    <AppModal
      :open="dialog.visible"
      size="md"
      :title="dialog.isEdit ? `编辑规则：${dialog.form.key}` : '新增规则'"
      @close="dialog.visible = false"
    >
      <div class="rules-form">
        <div class="modal-form__row">
          <FormField label="key（以 $ 开头）" :required="!dialog.isEdit">
            <AppInput v-model="dialog.form.key" :disabled="dialog.keyLocked" placeholder="$MY_RULE" />
          </FormField>
          <FormField label="名称（可选）">
            <AppInput v-model="dialog.form.label" placeholder="例如：综艺命名（含日期）" />
          </FormField>
        </div>
        <div v-if="!builtinKeySet.has(dialog.form.key)" class="modal-form__row">
          <FormField label="启用">
            <button
              type="button"
              class="drama-switch"
              :class="{ 'drama-switch--on': dialog.form.enabled }"
              @click="dialog.form.enabled = !dialog.form.enabled"
            >
              <span class="drama-switch__dot" />
              <span class="drama-switch__text">{{ dialog.form.enabled ? "启用" : "停用" }}</span>
            </button>
          </FormField>
          <FormField label="提示">
            <span class="rules-form__tip">key 必须以 $ 开头，且不能包含空格（最长 64）。</span>
          </FormField>
        </div>
        <div class="modal-form__row">
          <FormField label="pattern（正则表达式）" required>
            <AppInput
              v-model="dialog.form.pattern"
              placeholder="例如：^(?!.*纯享).*?第\d+期.*"
            />
          </FormField>
        </div>
        <div class="modal-form__row">
          <FormField label="replace（默认替换模板）">
            <AppInput v-model="dialog.form.replace" placeholder="{II}.{TASKNAME}.{DATE}.第{E}期{PART}.{EXT}" />
          </FormField>
        </div>
      </div>
      <template #footer>
        <div class="modal-form__footer">
          <AppButton type="button" variant="cancel" @click="dialog.visible = false">取消</AppButton>
          <AppButton type="button" variant="primary" :disabled="dialog.submitting" @click="submit">
            {{ dialog.submitting ? "保存中…" : "保存" }}
          </AppButton>
        </div>
      </template>
    </AppModal>
  </div>
</template>

<style scoped>
.rules-page {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.rules-page__hint {
  padding: 12px 16px;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-md);
  background: var(--surface);
  color: var(--text-muted);
  font-size: 12.5px;
  line-height: 1.8;
}

.rules-card {
  overflow: hidden;
}

.panel-head-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.rules-table {
  min-width: 860px;
  table-layout: fixed;
}

.rules-col-key { width: 16%; }
.rules-col-label { width: 16%; }
.rules-col-status { width: 12%; }
.rules-col-pattern { width: 24%; }
.rules-col-replace { width: 22%; }
.rules-col-op { width: 10%; }

.rules-table th.rules-col-op {
  text-align: center;
}

.rules-key {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-weight: 600;
  color: var(--brand);
}

.rules-label {
  color: var(--text-regular);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rules-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: var(--text-regular);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.rules-btn {
  padding: 5px 12px;
  border: 1px solid var(--border);
  border-radius: var(--radius-sm);
  background: var(--surface);
  color: var(--text-regular);
  font-size: 12.5px;
  cursor: pointer;
  transition: var(--transition);
}

.rules-btn:hover:not(:disabled) {
  border-color: var(--brand);
  color: var(--brand);
}

.rules-btn--danger:hover:not(:disabled) {
  border-color: var(--danger);
  color: var(--danger);
}

.rules-btn:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.rules-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.rules-form__tip {
  color: var(--text-muted);
  font-size: 12.5px;
  line-height: 1.6;
}

/* 复用任务表单开关样式（来自Trae） */
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
</style>
