<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-col gap-4">
          <div class="flex flex-col justify-between gap-4 xl:flex-row xl:items-center">
            <div class="flex flex-1 flex-wrap items-center gap-3">
              <div class="relative w-full sm:w-80">
                <Icon
                  name="search"
                  size="md"
                  class="absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
                />
                <input
                  v-model="filters.q"
                  type="text"
                  :placeholder="t('admin.conversationLogs.searchPlaceholder')"
                  class="input pl-10"
                  @input="onSearchInput"
                  @keyup.enter="applyFilters"
                />
              </div>

              <div class="w-full sm:w-52">
                <Input
                  v-model="filters.model"
                  :placeholder="t('admin.conversationLogs.modelPlaceholder')"
                  @enter="applyFilters"
                />
              </div>

              <Select
                v-model="filters.platform"
                :options="platformOptions"
                class="w-full sm:w-44"
                @change="applyFilters"
              />

              <Select
                v-model="filters.request_type"
                :options="requestTypeOptions"
                class="w-full sm:w-44"
                @change="applyFilters"
              />

              <Select
                v-model="filters.stream"
                :options="streamOptions"
                class="w-full sm:w-40"
                @change="applyFilters"
              />

              <DateRangePicker
                v-model:start-date="startDate"
                v-model:end-date="endDate"
                @change="applyFilters"
              />
            </div>

            <div class="flex flex-shrink-0 items-center justify-end gap-3">
              <button
                type="button"
                class="btn btn-secondary"
                :title="t('common.reset')"
                @click="resetFilters"
              >
                <Icon name="filter" size="md" class="mr-2" />
                {{ t('common.reset') }}
              </button>
              <button
                type="button"
                class="btn btn-secondary"
                :disabled="loading"
                :title="t('common.refresh')"
                @click="loadLogs"
              >
                <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <DataTable
          :columns="columns"
          :data="logs"
          :loading="loading"
          :server-side-sort="true"
          :default-sort-key="sortBy"
          :default-sort-order="sortOrder"
          row-key="id"
          sort-storage-key="admin-conversation-logs-sort"
          :estimate-row-height="76"
          @sort="handleSort"
        >
          <template #cell-created_at="{ row }">
            <div class="min-w-[9rem]">
              <div class="font-medium text-gray-900 dark:text-white">{{ formatDateTime(row.created_at) }}</div>
              <button
                type="button"
                class="mt-1 max-w-[9rem] truncate text-left text-xs text-gray-500 hover:text-primary-600 dark:text-gray-400 dark:hover:text-primary-400"
                :title="row.request_id"
                @click="copyText(row.request_id)"
              >
                {{ row.request_id || '-' }}
              </button>
            </div>
          </template>

          <template #cell-actor="{ row }">
            <div class="min-w-[12rem] max-w-[16rem]">
              <div class="truncate font-medium text-gray-900 dark:text-white" :title="actorTitle(row)">
                {{ displayUser(row) }}
              </div>
              <div class="truncate text-xs text-gray-500 dark:text-gray-400" :title="displayApiKey(row)">
                {{ displayApiKey(row) }}
              </div>
            </div>
          </template>

          <template #cell-route="{ row }">
            <div class="min-w-[12rem] max-w-[18rem] space-y-1">
              <div class="flex flex-wrap items-center gap-1.5">
                <span class="badge badge-primary">{{ row.platform || '-' }}</span>
                <span class="badge" :class="row.stream ? 'badge-success' : 'badge-secondary'">
                  {{ requestTypeLabel(row.request_type) }}
                </span>
              </div>
              <div class="truncate text-xs text-gray-500 dark:text-gray-400" :title="routeTitle(row)">
                {{ row.inbound_endpoint || '-' }}
              </div>
            </div>
          </template>

          <template #cell-model="{ row }">
            <div class="min-w-[11rem] max-w-[18rem]">
              <div class="truncate font-medium text-gray-900 dark:text-white" :title="row.model || row.requested_model">
                {{ row.model || row.requested_model || '-' }}
              </div>
              <div v-if="row.upstream_model && row.upstream_model !== row.model" class="truncate text-xs text-gray-500 dark:text-gray-400" :title="row.upstream_model">
                {{ row.upstream_model }}
              </div>
            </div>
          </template>

          <template #cell-status_code="{ row }">
            <span class="inline-flex rounded px-2 py-1 text-xs font-semibold" :class="statusClass(row.status_code)">
              {{ row.status_code || '-' }}
            </span>
          </template>

          <template #cell-total_tokens="{ row }">
            <div class="min-w-[6rem]">
              <div class="font-medium text-gray-900 dark:text-white">{{ formatNumber(row.total_tokens) }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">
                {{ formatNumber(row.input_tokens) }} / {{ formatNumber(row.output_tokens) }}
              </div>
            </div>
          </template>

          <template #cell-duration_ms="{ row }">
            <div class="min-w-[6rem]">
              <div class="font-medium text-gray-900 dark:text-white">{{ formatMs(row.duration_ms) }}</div>
              <div class="text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.conversationLogs.firstToken') }} {{ formatMs(row.first_token_ms) }}
              </div>
            </div>
          </template>

          <template #cell-actions="{ row }">
            <button
              type="button"
              class="btn btn-sm btn-secondary"
              :title="t('admin.conversationLogs.viewDetails')"
              @click="openDetail(row)"
            >
              <Icon name="eye" size="sm" class="mr-1.5" />
              {{ t('admin.conversationLogs.viewDetails') }}
            </button>
          </template>

          <template #empty>
            <div class="flex flex-col items-center py-4 text-center">
              <Icon name="chat" size="xl" class="mb-3 text-gray-300 dark:text-dark-500" />
              <p class="max-w-md text-sm text-gray-500 dark:text-gray-400">
                {{ t('admin.conversationLogs.noRecords') }}
              </p>
            </div>
          </template>
        </DataTable>
      </template>

      <template #pagination>
        <Pagination
          v-if="pagination.total > 0"
          :page="pagination.page"
          :total="pagination.total"
          :page-size="pagination.page_size"
          @update:page="handlePageChange"
          @update:pageSize="handlePageSizeChange"
        />
      </template>
    </TablePageLayout>
  </AppLayout>

  <BaseDialog
    :show="detailVisible"
    :title="t('admin.conversationLogs.details')"
    width="full"
    @close="closeDetail"
  >
    <div v-if="selectedLog" class="space-y-5">
      <div v-if="detailLoading" class="flex items-center gap-2 text-sm text-gray-500 dark:text-gray-400">
        <Icon name="refresh" size="sm" class="animate-spin" />
        {{ t('common.loading') }}
      </div>

      <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4">
        <DetailItem :label="t('admin.conversationLogs.user')" :value="displayUser(selectedLog)" />
        <DetailItem :label="t('admin.conversationLogs.apiKey')" :value="displayApiKey(selectedLog)" />
        <DetailItem :label="t('admin.conversationLogs.account')" :value="displayAccount(selectedLog)" />
        <DetailItem :label="t('admin.conversationLogs.group')" :value="displayGroup(selectedLog)" />
        <DetailItem :label="t('admin.conversationLogs.platform')" :value="selectedLog.platform || '-'" />
        <DetailItem :label="t('admin.conversationLogs.model')" :value="selectedLog.model || selectedLog.requested_model || '-'" />
        <DetailItem :label="t('admin.conversationLogs.requestType')" :value="requestTypeLabel(selectedLog.request_type)" />
        <DetailItem :label="t('admin.conversationLogs.status')" :value="String(selectedLog.status_code || '-')" />
        <DetailItem :label="t('admin.conversationLogs.inboundEndpoint')" :value="selectedLog.inbound_endpoint || '-'" />
        <DetailItem :label="t('admin.conversationLogs.upstreamEndpoint')" :value="selectedLog.upstream_endpoint || '-'" />
        <DetailItem :label="t('admin.conversationLogs.latency')" :value="formatMs(selectedLog.duration_ms)" />
        <DetailItem :label="t('admin.conversationLogs.queueDelay')" :value="formatMs(selectedLog.queue_delay_ms)" />
        <DetailItem :label="t('admin.conversationLogs.totalTokens')" :value="formatNumber(selectedLog.total_tokens)" />
        <DetailItem :label="t('admin.conversationLogs.cacheTokens')" :value="formatNumber(selectedLog.cache_read_tokens + selectedLog.cache_create_tokens)" />
        <DetailItem :label="t('admin.conversationLogs.requestHash')" :value="selectedLog.request_hash || '-'" />
        <DetailItem :label="t('admin.conversationLogs.responseId')" :value="selectedLog.response_id || '-'" />
      </div>

      <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <PayloadPanel
          :title="t('admin.conversationLogs.requestBody')"
          :body="selectedLog.request_body"
          :empty-text="t('admin.conversationLogs.noRequestBody')"
          :truncated="selectedLog.request_truncated"
          @copy="copyText(selectedLog.request_body)"
        />
        <PayloadPanel
          :title="t('admin.conversationLogs.responseBody')"
          :body="selectedLog.response_body"
          :empty-text="t('admin.conversationLogs.noResponseBody')"
          :truncated="selectedLog.response_truncated"
          @copy="copyText(selectedLog.response_body)"
        />
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, defineComponent, h, onMounted, onUnmounted, reactive, ref, type PropType, type VNodeChild } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import DateRangePicker from '@/components/common/DateRangePicker.vue'
import Input from '@/components/common/Input.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { getPersistedPageSize } from '@/composables/usePersistedPageSize'
import { adminConversationLogsAPI } from '@/api/admin/conversationLogs'
import type { ConversationLog, ConversationLogQueryParams } from '@/api/admin/conversationLogs'
import type { Column } from '@/components/common/types'
import type { UsageRequestType } from '@/types'

type SortOrder = 'asc' | 'desc'

const { t, locale } = useI18n()
const appStore = useAppStore()

const todayString = () => formatDateForInput(new Date())
const daysAgoString = (days: number) => {
  const date = new Date()
  date.setDate(date.getDate() - days)
  return formatDateForInput(date)
}

const startDate = ref(daysAgoString(6))
const endDate = ref(todayString())
const logs = ref<ConversationLog[]>([])
const loading = ref(false)
const detailLoading = ref(false)
const detailVisible = ref(false)
const selectedLog = ref<ConversationLog | null>(null)
const sortBy = ref('created_at')
const sortOrder = ref<SortOrder>('desc')
const pagination = reactive({
  page: 1,
  page_size: getPersistedPageSize(),
  total: 0,
  pages: 1
})

const filters = reactive<{
  q: string
  model: string
  platform: string | null
  request_type: UsageRequestType | null
  stream: boolean | null
}>({
  q: '',
  model: '',
  platform: null,
  request_type: null,
  stream: null
})

let abortController: AbortController | null = null
let searchTimer: number | null = null

const columns = computed<Column[]>(() => [
  { key: 'created_at', label: t('admin.conversationLogs.time'), sortable: true, class: 'min-w-[180px]' },
  { key: 'actor', label: t('admin.conversationLogs.actor'), class: 'min-w-[220px]' },
  { key: 'route', label: t('admin.conversationLogs.route'), class: 'min-w-[220px]' },
  { key: 'model', label: t('admin.conversationLogs.model'), class: 'min-w-[200px]' },
  { key: 'status_code', label: t('admin.conversationLogs.status'), sortable: true, class: 'min-w-[110px]' },
  { key: 'total_tokens', label: t('admin.conversationLogs.tokens'), class: 'min-w-[120px]' },
  { key: 'duration_ms', label: t('admin.conversationLogs.latency'), sortable: true, class: 'min-w-[140px]' },
  { key: 'actions', label: t('admin.conversationLogs.action'), class: 'min-w-[140px]' }
])

const platformOptions = computed(() => [
  { value: null, label: t('admin.conversationLogs.allPlatforms') },
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'grok', label: 'Grok' },
  { value: 'antigravity', label: 'Antigravity' }
])

const requestTypeOptions = computed(() => [
  { value: null, label: t('admin.conversationLogs.allTypes') },
  { value: 'sync', label: requestTypeLabel('sync') },
  { value: 'stream', label: requestTypeLabel('stream') },
  { value: 'ws_v2', label: requestTypeLabel('ws_v2') },
  { value: 'cyber', label: requestTypeLabel('cyber') }
])

const streamOptions = computed(() => [
  { value: null, label: t('admin.conversationLogs.allStreams') },
  { value: true, label: t('admin.conversationLogs.streamOnly') },
  { value: false, label: t('admin.conversationLogs.nonStreamOnly') }
])

async function loadLogs() {
  abortController?.abort()
  const controller = new AbortController()
  abortController = controller
  loading.value = true
  try {
    const params = buildQueryParams()
    const result = await adminConversationLogsAPI.list(params, { signal: controller.signal })
    if (controller.signal.aborted) return
    logs.value = result.items || []
    pagination.total = result.total || 0
    pagination.page = result.page || pagination.page
    pagination.page_size = result.page_size || pagination.page_size
    pagination.pages = result.pages || 1
  } catch (error: any) {
    if (isCancelError(error)) return
    appStore.showError(t('admin.conversationLogs.failedToLoad'))
  } finally {
    if (!controller.signal.aborted) {
      loading.value = false
    }
  }
}

function buildQueryParams(): ConversationLogQueryParams {
  const params: ConversationLogQueryParams = {
    page: pagination.page,
    page_size: pagination.page_size,
    start_date: startDate.value,
    end_date: endDate.value,
    sort_by: sortBy.value,
    sort_order: sortOrder.value
  }
  const q = filters.q.trim()
  const model = filters.model.trim()
  if (q) params.q = q
  if (model) params.model = model
  if (filters.platform) params.platform = filters.platform
  if (filters.request_type) params.request_type = filters.request_type
  if (filters.stream !== null) params.stream = filters.stream
  return params
}

function onSearchInput() {
  if (searchTimer) {
    window.clearTimeout(searchTimer)
  }
  searchTimer = window.setTimeout(() => {
    applyFilters()
  }, 350)
}

function applyFilters() {
  pagination.page = 1
  loadLogs()
}

function resetFilters() {
  filters.q = ''
  filters.model = ''
  filters.platform = null
  filters.request_type = null
  filters.stream = null
  startDate.value = daysAgoString(6)
  endDate.value = todayString()
  pagination.page = 1
  sortBy.value = 'created_at'
  sortOrder.value = 'desc'
  loadLogs()
}

function handlePageChange(page: number) {
  pagination.page = page
  loadLogs()
}

function handlePageSizeChange(pageSize: number) {
  pagination.page_size = pageSize
  pagination.page = 1
  loadLogs()
}

function handleSort(key: string, order: SortOrder) {
  sortBy.value = key
  sortOrder.value = order
  pagination.page = 1
  loadLogs()
}

async function openDetail(row: ConversationLog) {
  selectedLog.value = row
  detailVisible.value = true
  detailLoading.value = true
  try {
    const detail = await adminConversationLogsAPI.getById(row.id)
    if (selectedLog.value?.id === row.id) {
      selectedLog.value = detail
    }
  } catch {
    appStore.showError(t('admin.conversationLogs.failedToLoadDetail'))
  } finally {
    detailLoading.value = false
  }
}

function closeDetail() {
  detailVisible.value = false
  selectedLog.value = null
}

async function copyText(text: string) {
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    appStore.showSuccess(t('admin.conversationLogs.copied'))
  } catch {
    appStore.showError(t('common.copyFailed'))
  }
}

function displayUser(row: ConversationLog) {
  if (row.user_email) return row.user_email
  if (row.user_id) return `#${row.user_id}`
  return t('admin.conversationLogs.unknown')
}

function displayApiKey(row: ConversationLog) {
  if (row.api_key_name) return row.api_key_name
  if (row.api_key_id) return `#${row.api_key_id}`
  return '-'
}

function displayAccount(row: ConversationLog) {
  if (row.account_name) return row.account_name
  if (row.account_id) return `#${row.account_id}`
  return '-'
}

function displayGroup(row: ConversationLog) {
  if (row.group_name) return row.group_name
  if (row.group_id) return `#${row.group_id}`
  return '-'
}

function actorTitle(row: ConversationLog) {
  return `${displayUser(row)} / ${displayApiKey(row)}`
}

function routeTitle(row: ConversationLog) {
  return [row.inbound_endpoint, row.upstream_endpoint].filter(Boolean).join(' -> ')
}

function requestTypeLabel(type: UsageRequestType | string | null | undefined) {
  switch (type) {
    case 'sync':
      return t('usage.sync')
    case 'stream':
      return t('usage.stream')
    case 'ws_v2':
      return t('usage.ws')
    case 'cyber':
      return t('usage.cyber')
    default:
      return t('usage.unknown')
  }
}

function statusClass(status: number) {
  if (status >= 200 && status < 300) return 'bg-emerald-50 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300'
  if (status >= 400) return 'bg-rose-50 text-rose-700 dark:bg-rose-500/15 dark:text-rose-300'
  return 'bg-gray-100 text-gray-700 dark:bg-dark-700 dark:text-gray-300'
}

function formatDateTime(value: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return new Intl.DateTimeFormat(locale.value, {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit'
  }).format(date)
}

function formatDateForInput(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function formatMs(value: number | null | undefined) {
  if (value === null || value === undefined) return '-'
  if (value >= 1000) return `${(value / 1000).toFixed(value >= 10000 ? 1 : 2)}s`
  return `${value}ms`
}

function formatNumber(value: number | null | undefined) {
  return Number(value || 0).toLocaleString()
}

function isCancelError(error: any) {
  return error?.code === 'ERR_CANCELED' || error?.name === 'CanceledError'
}

const DetailItem = defineComponent({
  props: {
    label: { type: String, required: true },
    value: { type: String, required: true }
  },
  setup(props) {
    return () =>
      h('div', { class: 'rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-900' }, [
        h('div', { class: 'text-xs font-medium text-gray-500 dark:text-gray-400' }, props.label),
        h('div', { class: 'mt-1 break-words text-sm font-medium text-gray-900 dark:text-white', title: props.value }, props.value)
      ])
  }
})

type JsonKey = string | number

function parsePayload(body: string): { parsed: boolean; value: unknown; raw: string } {
  if (!body) return { parsed: false, value: null, raw: '' }
  const trimmed = body.trim()
  if (!trimmed) return { parsed: false, value: null, raw: '' }
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) {
    return { parsed: false, value: null, raw: body }
  }
  try {
    return { parsed: true, value: JSON.parse(trimmed), raw: body }
  } catch {
    return { parsed: false, value: null, raw: body }
  }
}

function isJsonRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function jsonContainerLabel(value: unknown) {
  if (Array.isArray(value)) {
    return `[${value.length}]`
  }
  if (isJsonRecord(value)) {
    return `{${Object.keys(value).length}}`
  }
  return ''
}

function jsonScalar(value: unknown) {
  if (value === null) {
    return { text: 'null', class: 'text-gray-500 dark:text-gray-400' }
  }
  if (typeof value === 'string') {
    return { text: JSON.stringify(value), class: 'text-emerald-700 dark:text-emerald-300' }
  }
  if (typeof value === 'number') {
    return { text: String(value), class: 'text-sky-700 dark:text-sky-300' }
  }
  if (typeof value === 'boolean') {
    return { text: String(value), class: 'text-amber-700 dark:text-amber-300' }
  }
  return { text: JSON.stringify(value), class: 'text-gray-800 dark:text-gray-100' }
}

const JsonTree = defineComponent({
  name: 'JsonTree',
  props: {
    value: { type: null as unknown as PropType<unknown>, required: true },
    nodeKey: { type: [String, Number] as PropType<JsonKey | undefined>, default: undefined },
    depth: { type: Number, default: 0 }
  },
  setup(props) {
    return () => renderJsonNode(props.value, props.nodeKey, props.depth)
  }
})

function renderJsonNode(value: unknown, nodeKey?: JsonKey, depth = 0): VNodeChild {
  if (Array.isArray(value) || isJsonRecord(value)) {
    const entries = Array.isArray(value)
      ? value.map((item, index) => [index, item] as const)
      : Object.entries(value)
    const isEmpty = entries.length === 0
    return h('details', { class: 'group/json rounded-md py-0.5', open: depth === 0 }, [
      h('summary', { class: 'cursor-pointer select-none rounded px-2 py-1 text-xs hover:bg-gray-50 dark:hover:bg-dark-800' }, [
        nodeKey !== undefined
          ? h('span', { class: 'mr-2 font-semibold text-indigo-700 dark:text-indigo-300' }, `${nodeKey}:`)
          : null,
        h('span', { class: 'font-semibold text-gray-700 dark:text-gray-200' }, jsonContainerLabel(value)),
        isEmpty
          ? h('span', { class: 'ml-2 text-gray-400 dark:text-gray-500' }, Array.isArray(value) ? '[]' : '{}')
          : null
      ]),
      isEmpty
        ? null
        : h('div', { class: 'ml-4 border-l border-gray-100 pl-2 dark:border-dark-700' }, entries.map(([key, child]) =>
            h(JsonTree, { key, value: child, nodeKey: key, depth: depth + 1 })
          ))
    ])
  }

  const scalar = jsonScalar(value)
  return h('div', { class: 'flex min-w-0 items-start gap-2 rounded px-2 py-0.5 text-xs' }, [
    nodeKey !== undefined
      ? h('span', { class: 'shrink-0 font-semibold text-indigo-700 dark:text-indigo-300' }, `${nodeKey}:`)
      : null,
    h('span', { class: ['min-w-0 break-words', scalar.class] }, scalar.text)
  ])
}

const PayloadPanel = defineComponent({
  props: {
    title: { type: String, required: true },
    body: { type: String, required: true },
    emptyText: { type: String, required: true },
    truncated: { type: Boolean, required: true }
  },
  emits: ['copy'],
  setup(props, { emit }) {
    return () => {
      const payload = parsePayload(props.body)
      const content = payload.raw
      return h('section', { class: 'overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-dark-700 dark:bg-dark-900' }, [
        h('div', { class: 'flex items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-700' }, [
          h('div', { class: 'min-w-0' }, [
            h('div', { class: 'flex min-w-0 items-center gap-2' }, [
              h('h3', { class: 'truncate text-sm font-semibold text-gray-900 dark:text-white' }, props.title),
              payload.parsed
                ? h('span', { class: 'rounded bg-indigo-50 px-1.5 py-0.5 text-[10px] font-semibold text-indigo-700 dark:bg-indigo-500/15 dark:text-indigo-300' }, 'JSON')
                : null
            ]),
            props.truncated
              ? h('p', { class: 'mt-1 text-xs text-amber-600 dark:text-amber-400' }, t('admin.conversationLogs.truncated'))
              : null
          ]),
          h(
            'button',
            {
              type: 'button',
              class: 'btn btn-sm btn-secondary flex-shrink-0',
              disabled: !content,
              onClick: () => emit('copy')
            },
            [h(Icon, { name: 'copy', size: 'sm', class: 'mr-1.5' }), t('admin.conversationLogs.copy')]
          )
        ]),
        content
          ? payload.parsed
            ? h('div', { class: 'max-h-[34rem] overflow-auto p-3 font-mono text-xs leading-relaxed' }, [
                h(JsonTree, { value: payload.value })
              ])
            : h('pre', { class: 'max-h-[34rem] overflow-auto whitespace-pre-wrap break-words p-4 text-xs leading-relaxed text-gray-800 dark:text-gray-100' }, content)
          : h('div', { class: 'p-6 text-sm text-gray-500 dark:text-gray-400' }, props.emptyText || t('admin.conversationLogs.bodyUnavailable'))
      ])
    }
  }
})

onMounted(() => {
  loadLogs()
})

onUnmounted(() => {
  abortController?.abort()
  if (searchTimer) {
    window.clearTimeout(searchTimer)
  }
})
</script>
