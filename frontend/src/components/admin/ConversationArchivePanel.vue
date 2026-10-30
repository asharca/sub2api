<template>
  <section class="rounded-lg border border-gray-200 p-4 dark:border-dark-700">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h3 class="font-medium">{{ text('title') }}</h3>
        <p class="mt-1 text-xs text-gray-500">{{ text('hint') }}</p>
      </div>
      <div class="flex flex-wrap gap-2">
        <button type="button" class="btn btn-secondary btn-sm" :disabled="creating || disabled" @click="create(false)">{{ text('export') }}</button>
        <button type="button" class="btn btn-danger btn-sm" :disabled="creating || disabled" @click="create(true)">{{ text('exportDelete') }}</button>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="loading" @click="refresh()">{{ t('common.refresh') }}</button>
      </div>
    </div>
    <p v-if="error" class="mt-2 text-sm text-red-600" role="alert">{{ error }}</p>
    <div v-if="records.length" class="mt-3 overflow-x-auto">
      <table class="w-full text-left text-xs">
        <thead><tr class="border-b dark:border-dark-700"><th class="p-2">{{ text('file') }}</th><th class="p-2">{{ text('status') }}</th><th class="p-2">{{ text('count') }}</th><th class="p-2">{{ text('deleted') }}</th><th class="p-2">{{ text('size') }}</th><th class="p-2">{{ text('actions') }}</th></tr></thead>
        <tbody>
          <tr v-for="record in records" :key="record.id" class="border-b dark:border-dark-700">
            <td class="max-w-xs p-2"><p>{{ record.file_name || record.id }}</p><p class="mt-1 text-gray-500">{{ new Date(record.started_at).toLocaleString() }}</p><details class="mt-1"><summary>{{ text('details') }}</summary><pre class="mt-1 whitespace-pre-wrap break-all">{{ JSON.stringify(record.filters, null, 2) }}</pre><p class="break-all">SHA256: {{ record.sha256 || '—' }}</p></details></td>
            <td class="max-w-xs p-2"><p :class="record.status === 'failed' || record.status === 'cleanup_failed' ? 'text-red-600' : ''">{{ text(record.status) }}</p><p v-if="record.error_message" class="mt-1 break-words text-red-600">{{ record.error_message }}</p></td>
            <td class="p-2">{{ record.record_count }}</td><td class="p-2">{{ record.deleted_count }}</td><td class="p-2">{{ (record.size_bytes / 1024 / 1024).toFixed(2) }} MiB</td>
            <td class="p-2"><button v-if="record.status === 'completed' || record.status === 'cleanup_failed'" type="button" class="btn btn-secondary btn-xs" @click="download(record.id)">{{ t('admin.backup.actions.download') }}</button></td>
          </tr>
        </tbody>
      </table>
    </div>
    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { conversationArchivesAPI, type ConversationArchive } from '@/api/admin/conversationArchives'
import type { ConversationLogQueryParams } from '@/api/admin/conversationLogs'
import { useStepUp, isStepUpCancelled, isStepUpBlocked, stepUpBlockReason } from '@/composables/useStepUp'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'

const props = defineProps<{ getFilters: () => ConversationLogQueryParams; disabled?: boolean }>()
const emit = defineEmits<{ cleaned: [] }>()
const { t } = useI18n()
const text = (key: string) => t(`admin.backup.conversationArchive.${key}`)
const stepUp = useStepUp()
const records = ref<ConversationArchive[]>([])
const creating = ref(false)
const loading = ref(false)
const error = ref('')
let disposed = false
let timer: ReturnType<typeof setTimeout> | undefined

function report(err: unknown) {
  if (isStepUpCancelled(err)) return
  error.value = isStepUpBlocked(err)
    ? t(stepUpBlockReason(err) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
    : (err as { message?: string })?.message || t('errors.networkError')
}

async function refresh() {
  if (loading.value || disposed) return
  clearTimeout(timer)
  loading.value = true
  try {
    const next = await conversationArchivesAPI.list()
    if (disposed) return
    const cleaning = new Set(records.value.filter(record => record.status === 'running' && record.delete_after_backup).map(record => record.id))
    records.value = next
    if (next.some(record => cleaning.has(record.id) && record.status !== 'running' && record.deleted_count > 0)) emit('cleaned')
  } catch (err) {
    report(err)
  } finally {
    loading.value = false
    if (!disposed && records.value.some(record => record.status === 'running')) timer = setTimeout(refresh, 2000)
  }
}

async function create(deleteAfterBackup: boolean) {
  if (creating.value || props.disabled) return
  const { page: _page, page_size: _size, sort_by: _sort, sort_order: _order, ...filters } = props.getFilters()
  if (deleteAfterBackup && !window.confirm(text('confirmDelete'))) return
  creating.value = true
  error.value = ''
  try {
    const record = await stepUp.run(() => conversationArchivesAPI.create(filters, deleteAfterBackup))
    if (disposed) return
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

onMounted(() => { void refresh() })
onUnmounted(() => { disposed = true; clearTimeout(timer) })
</script>
