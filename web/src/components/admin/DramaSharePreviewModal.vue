<script setup lang="ts">
// 追剧分享链接预览弹窗（来自Trae）。
// 复刻 CASX DramaTaskDrawer 的「选择需转存的文件夹」弹窗：
// 支持按层级点击浏览分享目录、查看哪些文件能转存、以及规则重命名后的目标文件名。
// 浏览通过后端 /admin/drama/share/preview 的 pdir_fid 参数逐级拉取。
import { ref, watch } from "vue";
import { getApiErrorMessage } from "@/api/client";
import { previewShare, type SharePreviewItem } from "@/api/drama";
import AppModal from "@/components/base/AppModal.vue";
import AppButton from "@/components/base/AppButton.vue";
import { toast } from "@/composables/useToast";
import { formatSize } from "@/utils/format";

const props = withDefaults(
  defineProps<{
    open: boolean;
    accountId: number;
    shareUrl: string; // 根分享链接
    taskName?: string;
    pattern?: string;
    replace?: string;
    sortIndex?: number;
    savePath?: string;
    ignoreExtension?: boolean;
    updateSubdir?: string;
    startFid?: string;
  }>(),
  {
    taskName: "",
    pattern: "",
    replace: "",
    sortIndex: 0,
    savePath: "",
    ignoreExtension: false,
    updateSubdir: "",
    startFid: "",
  },
);

const emit = defineEmits<{
  close: [];
  // 选中当前目录：返回重构后的子目录分享链接
  select: [payload: { shareUrl: string; fid: string; name: string }];
}>();

const loading = ref(false);
const items = ref<SharePreviewItem[]>([]);
const driveType = ref("");
const rootShareUrl = ref("");
// 浏览栈：首项为根目录，后续为逐级进入的子目录（来自Trae）
const stack = ref<{ name: string; fid: string }[]>([]);
const currentFid = ref("");

// 从分享链接中解析起始 fid（与后端 ExtractShareURL 解析规则对齐）（来自Trae）
function extractShareFid(url: string): string {
  const raw = String(url || "").trim();
  const mq = raw.match(/(?:\?|&)(?:parentId|parent_id|pdir_fid|fid|fileId)=([^&#]+)/);
  if (mq?.[1] && !["0", "root"].includes(String(mq[1]).trim())) return String(mq[1]).trim();
  const m2 = raw.match(/\/([a-fA-F0-9]{32})-?[^/]*$/);
  if (m2?.[1]) return m2[1];
  return "";
}

// 重构子目录分享链接（与后端各驱动 ExtractShareURL 解析规则对齐）（来自Trae）
function buildShareSubUrl(rootUrl: string, fid: string): string {
  const raw = String(rootUrl || "").trim();
  const dfid = String(fid || "").trim();
  const type = String(driveType.value || "").toLowerCase();
  const stripSub = (s: string) =>
    s.replace(/\/([a-fA-F0-9]{32})-?[^/]*$/, "").replace(/([?&])parentId=[^&#]*/g, "$1").replace(/([?&])fid=[^&#]*/g, "$1").replace(/[?&]+$/, "").replace("?&", "?").replace(/&$/, "");
  if (!dfid || dfid === "0") {
    return stripSub(raw);
  }
  if (type.includes("quark")) {
    const base = stripSub(raw).split("?")[0].split("#")[0];
    return `${base}/${dfid}`;
  }
  if (type.includes("guangya")) {
    const base = stripSub(raw);
    return `${base}${base.includes("?") ? "&" : "?"}parentId=${encodeURIComponent(dfid)}`;
  }
  // 未知驱动：尽力按查询参数追加（来自Trae）
  return `${raw.split("#")[0]}${raw.includes("?") ? "&" : "?"}fid=${encodeURIComponent(dfid)}`;
}

function currentPathLabel(): string {
  const parts = stack.value.map((x) => x.name).filter(Boolean);
  return parts.length ? `/${parts.join("/")}` : "/";
}

// 拉取当前层级（来自Trae）
async function refresh(fid?: string) {
  if (!props.accountId) return;
  loading.value = true;
  try {
    const data = await previewShare({
      account_id: props.accountId,
      share_url: rootShareUrl.value,
      pdir_fid: fid || undefined,
      max_items: 200,
      task_name: props.taskName || undefined,
      pattern: props.pattern || undefined,
      replace: props.replace || undefined,
      sort_index: props.sortIndex || undefined,
      save_path: props.savePath || undefined,
      ignore_extension: props.ignoreExtension,
      update_subdir: props.updateSubdir || undefined,
      start_fid: props.startFid || undefined,
    });
    driveType.value = data.drive_type || "";
    currentFid.value = data.pdir_fid || fid || "";
    items.value = data.items || [];
  } catch (e) {
    toast.error(getApiErrorMessage(e, "预览失败"));
    items.value = [];
  } finally {
    loading.value = false;
  }
}

function openRoot() {
  rootShareUrl.value = props.shareUrl.trim();
  const fid = extractShareFid(props.shareUrl);
  stack.value = fid ? [{ name: "当前目录", fid }] : [];
  void refresh(fid || undefined);
}

function enterDir(item: SharePreviewItem) {
  if (!item.is_dir) return;
  stack.value = [...stack.value, { name: item.name, fid: item.fid }];
  void refresh(item.fid);
}

function goBack() {
  if (stack.value.length <= 1) {
    openRoot();
    return;
  }
  stack.value = stack.value.slice(0, -1);
  const target = stack.value[stack.value.length - 1];
  void refresh(target?.fid || undefined);
}

function goRoot() {
  stack.value = [];
  void refresh();
}

function useCurrentFolder() {
  const target = stack.value[stack.value.length - 1];
  const fid = target?.fid && target.fid !== "0" ? target.fid : "";
  const name = target?.name || "/";
  if (!fid) {
    toast.warning("请先进入某个文件夹后再选择");
    return;
  }
  const shareUrl = buildShareSubUrl(rootShareUrl.value, fid);
  emit("select", { shareUrl, fid, name });
  emit("close");
}

function formatTs(value: number): string {
  if (!value) return "-";
  const sec = value < 1e12 ? value : value / 1000;
  const d = new Date(sec * 1000);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

watch(
  () => props.open,
  (open) => {
    if (open) openRoot();
  },
);
</script>

<template>
  <AppModal :open="open" size="branch" title="预览分享目录" @close="emit('close')">
    <div class="preview">
      <div class="preview__toolbar">
        <div class="preview__actions">
          <AppButton type="button" size="sm" variant="secondary" :disabled="loading" @click="refresh(currentFid || undefined)">
            <i class="fas fa-sync"></i>
            刷新
          </AppButton>
          <AppButton type="button" size="sm" variant="secondary" :disabled="loading" @click="goBack">
            <i class="fas fa-arrow-up"></i>
            返回上级
          </AppButton>
          <AppButton type="button" size="sm" variant="secondary" :disabled="loading || stack.length === 0" @click="goRoot">
            <i class="fas fa-home"></i>
            根目录
          </AppButton>
          <AppButton type="button" size="sm" variant="primary" :disabled="stack.length === 0" @click="useCurrentFolder">
            使用当前文件夹
          </AppButton>
        </div>
        <div class="preview__path" :title="currentPathLabel()">当前路径：{{ currentPathLabel() }}</div>
      </div>

      <div class="preview__table-wrap">
        <table class="admin-table preview-table">
          <thead>
            <tr>
              <th class="preview-col-name">名称</th>
              <th class="preview-col-size">大小</th>
              <th class="preview-col-re">正则处理</th>
              <th class="preview-col-time">时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="loading">
              <td colspan="4" class="empty-cell">加载中...</td>
            </tr>
            <tr v-else-if="items.length === 0">
              <td colspan="4" class="empty-cell">该目录下没有条目，或分享链接不可访问</td>
            </tr>
            <tr
              v-for="item in items"
              v-else
              :key="item.fid"
              class="preview-row"
              :class="{ 'preview-row--dir': item.is_dir }"
              @click="enterDir(item)"
            >
              <td>
                <span class="preview-name">{{ item.name }}</span>
                <span class="preview-tag" :class="item.is_dir ? 'preview-tag--dir' : 'preview-tag--file'">
                  {{ item.is_dir ? "目录" : "文件" }}
                </span>
              </td>
              <td class="preview-size">
                <template v-if="item.is_dir">
                  {{ item.children_count != null ? `${item.children_count} 项` : "-" }}
                </template>
                <template v-else>{{ formatSize(item.size) }}</template>
              </td>
              <td class="preview-re">
                <span v-if="item.name_re" class="preview-re__renamed" :title="item.name_re">{{ item.name_re }}</span>
                <span v-else-if="item.name_saved" class="preview-re__saved" :title="item.name_saved">{{ item.name_saved }}</span>
                <span v-else class="preview-re__skip">x</span>
              </td>
              <td class="preview-time">{{ formatTs(item.updated_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </AppModal>
</template>

<style scoped>
.preview {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.preview__toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--border-soft);
  margin-bottom: 12px;
}

.preview__actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.preview__path {
  color: var(--text-muted);
  font-size: 12.5px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 60%;
}

.preview__table-wrap {
  flex: 1;
  min-height: 0;
  overflow: auto;
  border: 1px solid var(--border-soft);
  border-radius: var(--radius-md);
}

.preview-table {
  table-layout: fixed;
}

.preview-col-name { width: 42%; }
.preview-col-size { width: 14%; }
.preview-col-re { width: 30%; }
.preview-col-time { width: 14%; }

.preview-row {
  cursor: default;
}

.preview-row--dir {
  cursor: pointer;
}

.preview-row--dir:hover .preview-name {
  color: var(--brand);
}

.preview-name {
  color: var(--text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.preview-tag {
  margin-left: 8px;
  padding: 1px 8px;
  border-radius: var(--radius-pill);
  font-size: 11.5px;
  font-weight: 600;
  flex-shrink: 0;
}

.preview-tag--dir {
  background: var(--surface-sunken);
  color: var(--text-muted);
}

.preview-tag--file {
  background: color-mix(in srgb, var(--success) 12%, transparent);
  color: var(--success);
}

.preview-size,
.preview-time {
  color: var(--text-muted);
  font-size: 12.5px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.preview-re {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.preview-re__renamed {
  color: var(--success);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
}

.preview-re__saved {
  color: var(--text-muted);
  font-size: 12.5px;
}

.preview-re__skip {
  color: var(--danger);
  font-weight: 700;
}
</style>
