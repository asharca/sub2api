<template>
  <button
    type="button"
    class="btn btn-secondary"
    aria-haspopup="dialog"
    :aria-expanded="visible"
    @click="open"
  >
    {{ text('title') }}
  </button>
  <BaseDialog
    :show="visible"
    :title="text('title')"
    width="extra-wide"
    close-on-click-outside
    @close="visible = false"
  >
    <p class="text-xs text-gray-500">{{ text('hint') }}</p>
    <div class="mt-4 flex flex-wrap gap-2">
      <button type="button" class="btn btn-primary btn-sm" :disabled="downloading || creating || busyId !== '' || disabled" @click="exportToBrowser()">{{ text(downloading ? 'downloading' : 'browserDownload') }}</button>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="creating || downloading || busyId !== '' || disabled" @click="create(false)">{{ text('export') }}</button>
      <button type="button" class="btn btn-danger btn-sm" :disabled="creating || downloading || busyId !== '' || disabled" @click="create(true)">{{ text('exportDelete') }}</button>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="refresh()">{{ t('common.refresh') }}</button>
    </div>
    <p v-if="error" class="mt-2 text-sm text-red-600" role="alert">{{ error }}</p>
    <div v-if="records.length" class="mt-3 overflow-x-auto">
      <table class="min-w-[720px] w-full text-left text-xs">
        <thead><tr class="border-b dark:border-dark-700"><th class="p-2">{{ text('file') }}</th><th class="p-2">{{ text('status') }}</th><th class="p-2">{{ text('count') }}</th><th class="p-2">{{ text('deleted') }}</th><th class="p-2">{{ text('size') }}</th><th class="p-2">{{ text('actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="record in records" :key="record.id" class="border-b dark:border-dark-700">
            <td class="max-w-xs p-2"><p>{{ record.file_name || record.id }}</p><p class="mt-1 text-gray-500">{{ new Date(record.started_at).toLocaleString() }}</p><details class="mt-1"><summary>{{ text('details') }}</summary><pre class="mt-1 whitespace-pre-wrap break-all">{{ JSON.stringify(record.filters, null, 2) }}</pre><p class="break-all">SHA256: {{ record.sha256 || '—' }}</p></details></td>
            <td class="max-w-xs p-2"><p :class="record.status === 'failed' || record.status === 'cleanup_failed' ? 'text-red-600' : ''">{{ text(record.status) }}</p><p v-if="record.error_message" class="mt-1 break-words text-red-600">{{ record.error_message }}</p><p v-if="isTooLarge(record)" class="mt-1 break-words text-gray-500">{{ text('tooLargeHint') }}</p></td>
            <td class="p-2">{{ record.record_count }}</td><td class="p-2">{{ record.deleted_count }}</td><td class="p-2">{{ (record.size_bytes / 1024 / 1024).toFixed(2) }} MiB</td>
            <td class="min-w-[10rem] p-2"><div class="flex flex-wrap gap-1">
              <button v-if="record.status === 'completed' || record.status === 'cleanup_failed'" type="button" class="btn btn-secondary btn-xs" :disabled="busyId !== ''" @click="download(record.id)">{{ t('admin.backup.actions.download') }}</button>
              <button v-if="record.status === 'failed'" type="button" class="btn btn-secondary btn-xs" :disabled="busyId !== '' || creating || downloading || disabled" @click="retry(record)">{{ text(busyId === record.id && busyAction === 'retry' ? 'retrying' : 'retry') }}</button>
              <button v-if="record.status !== 'running'" type="button" class="btn btn-danger btn-xs" :disabled="busyId !== ''" @click="remove(record)">{{ text(busyId === record.id && busyAction === 'remove' ? 'removing' : 'remove') }}</button>
            </div></td>
          </tr>
        </tbody>
      </table>
    </div>
    <template #footer>
      <button type="button" class="btn btn-secondary" @click="visible = false">{{ t('common.close') }}</button>
    </template>
  </BaseDialog>
  <TotpStepUpDialog :controller="stepUp" />
</template>

<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { conversationArchivesAPI, type ConversationArchive } from '@/api/admin/conversationArchives'
import type { ConversationLogQueryParams } from '@/api/admin/conversationLogs'
import { useStepUp, isStepUpCancelled, isStepUpBlocked, stepUpBlockReason } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ getFilters: () => ConversationLogQueryParams; disabled?: boolean }>()
const emit = defineEmits<{ cleaned: [] }>()
const { t } = useI18n()
const text = (key: string) => t(`admin.backup.conversationArchive.${key}`)
const stepUp = useStepUp()
const visible = ref(false)
const records = ref<ConversationArchive[]>([])
const creating = ref(false)
const downloading = ref(false)
const loading = ref(false)
const error = ref('')
const busyId = ref('')
const busyAction = ref<'retry' | 'remove' | ''>('')
let disposed = false
let timer: ReturnType<typeof setTimeout> | undefined
let catalogRevision = 0
let refreshQueued = false
const downloadURLs = new Map<string, ReturnType<typeof setTimeout>>()

function report(err: unknown) {
  if (isStepUpCancelled(err)) return
  error.value = isStepUpBlocked(err)
    ? t(stepUpBlockReason(err) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
    : (err as { message?: string })?.message || t('errors.networkError')
}

function open() {
  visible.value = true
  void refresh()
}

async function refresh() {
  if (disposed) return
  if (loading.value) {
    refreshQueued = true
    return
  }
  clearTimeout(timer)
  loading.value = true
  const revision = catalogRevision
  try {
    const next = await conversationArchivesAPI.list()
    if (disposed || revision !== catalogRevision) return
    const cleaning = new Set(records.value.filter(record => record.status === 'running' && record.delete_after_backup).map(record => record.id))
    records.value = next
    if (next.some(record => cleaning.has(record.id) && record.status !== 'running' && record.deleted_count > 0)) emit('cleaned')
  } catch (err) {
    report(err)
  } finally {
    loading.value = false
    if (!disposed && refreshQueued) {
      refreshQueued = false
      void refresh()
    } else if (!disposed && records.value.some(record => record.status === 'running')) {
      timer = setTimeout(refresh, 2000)
    }
  }
}

function currentFilters() {
  const { page: _page, page_size: _size, sort_by: _sort, sort_order: _order, ...filters } = props.getFilters()
  return filters
}

async function exportToBrowser() {
  if (downloading.value || creating.value || busyId.value || props.disabled || disposed) return
  const filters = currentFilters()
  downloading.value = true
  error.value = ''
  try {
    const { blob, fileName } = await stepUp.run(() => conversationArchivesAPI.export(filters))
    if (disposed) return
    const url = URL.createObjectURL(blob)
    const link = document.createElement('a')
    try {
      link.href = url
      link.download = fileName
      document.body.appendChild(link)
      link.click()
    } finally {
      link.remove()
      // Give the browser time to accept the download before releasing its data.
      downloadURLs.set(url, setTimeout(() => {
        URL.revokeObjectURL(url)
        downloadURLs.delete(url)
      }, 60_000))
    }
  } catch (err) {
    report(err)
  } finally {
    downloading.value = false
  }
}

async function create(deleteAfterBackup: boolean) {
  if (creating.value || downloading.value || busyId.value || props.disabled) return
  const filters = currentFilters()
  if (deleteAfterBackup && !window.confirm(text('confirmDelete'))) return
  creating.value = true
  error.value = ''
  try {
    const record = await stepUp.run(() => conversationArchivesAPI.create(filters, deleteAfterBackup))
    if (disposed) return
    catalogRevision += 1
    records.value.unshift(record)
    await refresh()
  } catch (err) {
    report(err)
  } finally {
    creating.value = false
  }
}

async function download(id: string) {
  error.value = ''
  try {
    const url = await stepUp.run(() => conversationArchivesAPI.download(id))
    if (!url) throw new Error(t('admin.backup.actions.downloadFailed'))
    const link = document.createElement('a')
    link.href = url
    link.rel = 'noopener'
    link.click()
  } catch (err) {
    report(err)
  }
}

function isTooLarge(record: ConversationArchive) {
  return record.status === 'failed' && /EntityTooLarge|maximum object size/i.test(record.error_message || '')
}

async function retry(record: ConversationArchive) {
  if (busyId.value || creating.value || downloading.value || props.disabled || disposed || record.status !== 'failed') return
  if (!window.confirm(text(record.delete_after_backup ? 'confirmRetryDelete' : 'confirmRetry'))) return
  busyId.value = record.id
  busyAction.value = 'retry'
  error.value = ''
  try {
    const next = await stepUp.run(() => conversationArchivesAPI.retry(record.id))
    if (disposed) return
    catalogRevision += 1
    records.value.unshift(next)
    await refresh()
  } catch (err) {
    report(err)
  } finally {
    busyId.value = ''
    busyAction.value = ''
  }
}

async function remove(record: ConversationArchive) {
  if (busyId.value || disposed || record.status === 'running') return
  if (!window.confirm(text(record.deleted_count > 0 ? 'confirmRemoveCleaned' : 'confirmRemove'))) return
  busyId.value = record.id
  busyAction.value = 'remove'
  error.value = ''
  try {
    await stepUp.run(() => conversationArchivesAPI.remove(record.id))
    if (disposed) return
    catalogRevision += 1
    records.value = records.value.filter(item => item.id !== record.id)
    await refresh()
  } catch (err) {
    report(err)
  } finally {
    busyId.value = ''
    busyAction.value = ''
  }
}

onUnmounted(() => {
  disposed = true
  clearTimeout(timer)
  for (const [url, revokeTimer] of downloadURLs) {
    clearTimeout(revokeTimer)
    URL.revokeObjectURL(url)
  }
  downloadURLs.clear()
})
</script>
