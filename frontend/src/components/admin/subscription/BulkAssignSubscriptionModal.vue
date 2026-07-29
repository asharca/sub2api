<template>
  <BaseDialog
    :show="show"
    :title="t('admin.subscriptions.bulkAssign.title')"
    width="wide"
    :close-on-escape="!submitting"
    @close="handleClose"
  >
    <form id="bulk-assign-subscription-form" class="space-y-5" @submit.prevent="handleSubmit">
      <section aria-labelledby="bulk-assign-users-label">
        <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
          <div>
            <label id="bulk-assign-users-label" for="bulk-assign-user-search" class="input-label mb-0">
              {{ t('admin.subscriptions.form.users') }}
            </label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.subscriptions.bulkAssign.selectionHint') }}
            </p>
          </div>
          <div class="flex items-center gap-2">
            <span
              class="rounded-full bg-primary-50 px-2.5 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
              data-test="selected-count"
            >
              {{ t('admin.subscriptions.bulkAssign.selectedCount', { count: selectedIds.length }) }}
            </span>
            <button
              v-if="selectedIds.length > 0"
              type="button"
              class="text-xs font-medium text-gray-500 hover:text-gray-800 dark:text-gray-400 dark:hover:text-gray-200"
              data-test="clear-selection"
              @click="clearSelection"
            >
              {{ t('admin.subscriptions.bulkAssign.clearSelection') }}
            </button>
          </div>
        </div>

        <div class="relative mb-3">
          <Icon
            name="search"
            size="md"
            class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
          />
          <input
            id="bulk-assign-user-search"
            v-model="searchQuery"
            type="search"
            class="input pl-10"
            :placeholder="t('admin.subscriptions.bulkAssign.searchPlaceholder')"
            data-test="user-search"
            @input="handleSearchInput"
          />
        </div>

        <div class="overflow-hidden rounded-xl border border-gray-200 dark:border-dark-700">
          <div
            class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-200 bg-gray-50 px-4 py-2.5 dark:border-dark-700 dark:bg-dark-800/70"
          >
            <span class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.subscriptions.bulkAssign.pageSummary', {
                start: pageStart,
                end: pageEnd,
                total: pagination.total
              }) }}
            </span>
            <button
              v-if="users.length > 0"
              type="button"
              class="text-xs font-semibold text-primary-600 hover:text-primary-700 dark:text-primary-400 dark:hover:text-primary-300"
              data-test="toggle-current-page"
              @click="toggleCurrentPage"
            >
              {{ allCurrentPageSelected
                ? t('admin.subscriptions.bulkAssign.unselectCurrentPage')
                : t('admin.subscriptions.bulkAssign.selectCurrentPage') }}
            </button>
          </div>

          <div class="max-h-72 overflow-y-auto" aria-live="polite">
            <div
              v-if="loadingUsers"
              class="flex min-h-40 items-center justify-center gap-2 text-sm text-gray-500 dark:text-gray-400"
              data-test="users-loading"
            >
              <Icon name="refresh" size="md" class="animate-spin" />
              {{ t('common.loading') }}
            </div>
            <div
              v-else-if="userLoadError"
              class="flex min-h-40 flex-col items-center justify-center gap-3 px-6 text-center"
              data-test="users-error"
            >
              <p class="text-sm text-red-600 dark:text-red-400">{{ userLoadError }}</p>
              <button type="button" class="btn btn-secondary btn-sm" @click="loadUsers">
                {{ t('admin.subscriptions.bulkAssign.retryLoad') }}
              </button>
            </div>
            <div
              v-else-if="users.length === 0"
              class="flex min-h-40 items-center justify-center px-6 text-sm text-gray-500 dark:text-gray-400"
              data-test="users-empty"
            >
              {{ t('admin.subscriptions.bulkAssign.noUsers') }}
            </div>
            <label
              v-for="user in users"
              v-else
              :key="user.id"
              class="flex cursor-pointer items-center gap-3 border-b border-gray-100 px-4 py-3 last:border-b-0 hover:bg-gray-50 dark:border-dark-700/70 dark:hover:bg-dark-800/60"
              :data-test="`user-row-${user.id}`"
            >
              <input
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-800"
                :checked="isSelected(user.id)"
                :aria-label="t('admin.subscriptions.bulkAssign.selectUser', { email: user.email })"
                @change="toggleUser(user.id)"
              />
              <span class="min-w-0 flex-1">
                <span class="block truncate text-sm font-medium text-gray-900 dark:text-white">
                  {{ user.email }}
                </span>
                <span class="block truncate text-xs text-gray-500 dark:text-gray-400">
                  {{ user.username || `#${user.id}` }}
                </span>
              </span>
              <span
                :class="[
                  'rounded-full px-2 py-0.5 text-[11px] font-medium',
                  user.status === 'active'
                    ? 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-300'
                    : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
                ]"
              >
                {{ user.status === 'active' ? t('common.active') : t('admin.users.disabled') }}
              </span>
            </label>
          </div>

          <div
            v-if="pagination.pages > 1"
            class="flex items-center justify-between border-t border-gray-200 px-4 py-2.5 dark:border-dark-700"
          >
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="pagination.page <= 1 || loadingUsers"
              data-test="previous-page"
              @click="goToPage(pagination.page - 1)"
            >
              {{ t('admin.subscriptions.bulkAssign.previousPage') }}
            </button>
            <span class="text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.subscriptions.bulkAssign.pageNumber', {
                page: pagination.page,
                pages: pagination.pages
              }) }}
            </span>
            <button
              type="button"
              class="btn btn-secondary btn-sm"
              :disabled="pagination.page >= pagination.pages || loadingUsers"
              data-test="next-page"
              @click="goToPage(pagination.page + 1)"
            >
              {{ t('admin.subscriptions.bulkAssign.nextPage') }}
            </button>
          </div>
        </div>
      </section>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div>
          <label class="input-label">{{ t('admin.subscriptions.form.group') }}</label>
          <Select
            v-model="groupId"
            :options="subscriptionGroupOptions"
            :placeholder="t('admin.subscriptions.selectGroup')"
            data-test="group-select"
          >
            <template #selected="{ option }">
              <GroupBadge
                v-if="option"
                :name="(option as unknown as GroupOption).label"
                :platform="(option as unknown as GroupOption).platform"
                :subscription-type="(option as unknown as GroupOption).subscriptionType"
                :rate-multiplier="(option as unknown as GroupOption).rate"
              />
              <span v-else class="text-gray-400">{{ t('admin.subscriptions.selectGroup') }}</span>
            </template>
            <template #option="{ option, selected }">
              <GroupOptionItem
                :name="(option as unknown as GroupOption).label"
                :platform="(option as unknown as GroupOption).platform"
                :subscription-type="(option as unknown as GroupOption).subscriptionType"
                :rate-multiplier="(option as unknown as GroupOption).rate"
                :description="(option as unknown as GroupOption).description"
                :selected="selected"
              />
            </template>
          </Select>
          <p class="input-hint">{{ t('admin.subscriptions.groupHint') }}</p>
        </div>
        <div>
          <label for="bulk-assign-validity-days" class="input-label">
            {{ t('admin.subscriptions.form.validityDays') }}
          </label>
          <input
            id="bulk-assign-validity-days"
            v-model.number="validityDays"
            type="number"
            min="1"
            max="36500"
            step="1"
            class="input"
            data-test="validity-days"
          />
          <p class="input-hint">{{ t('admin.subscriptions.validityHint') }}</p>
        </div>
      </div>

      <p v-if="selectionTooLarge" class="text-sm text-red-600 dark:text-red-400">
        {{ t('admin.subscriptions.bulkAssign.selectionLimit', { max: MAX_SELECTED_USERS }) }}
      </p>

      <div
        v-if="assignmentResult"
        class="rounded-xl border border-amber-200 bg-amber-50 p-4 dark:border-amber-800/60 dark:bg-amber-900/20"
        role="status"
        data-test="assignment-result"
      >
        <p class="text-sm font-semibold text-amber-900 dark:text-amber-200">
          {{ t('admin.subscriptions.bulkAssign.partialResult', {
            success: assignmentResult.success_count,
            failed: assignmentResult.failed_count,
            created: assignmentResult.created_count,
            reused: assignmentResult.reused_count
          }) }}
        </p>
        <ul
          v-if="assignmentResult.errors.length > 0"
          class="mt-2 max-h-28 list-disc space-y-1 overflow-y-auto pl-5 text-xs text-amber-800 dark:text-amber-300"
        >
          <li v-for="(error, index) in assignmentResult.errors" :key="index">{{ error }}</li>
        </ul>
      </div>
    </form>

    <template #footer>
      <div class="flex flex-wrap items-center justify-end gap-3">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="bulk-assign-subscription-form"
          class="btn btn-primary"
          :disabled="!canSubmit"
          data-test="submit"
        >
          <Icon v-if="submitting" name="refresh" size="sm" class="mr-2 animate-spin" />
          {{ submitting
            ? t('admin.subscriptions.assigning')
            : t('admin.subscriptions.bulkAssign.submit', { count: selectedIds.length }) }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type {
  AdminGroup,
  AdminUser,
  BulkAssignSubscriptionResult,
  GroupPlatform,
  SubscriptionType
} from '@/types'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

interface GroupOption extends Record<string, unknown> {
  value: number
  label: string
  description: string | null
  platform: GroupPlatform
  subscriptionType: SubscriptionType
  rate: number
}

const props = defineProps<{
  show: boolean
  groups: AdminGroup[]
}>()

const emit = defineEmits<{
  close: []
  success: [result: BulkAssignSubscriptionResult]
}>()

const { t } = useI18n()
const appStore = useAppStore()
const PAGE_SIZE = 20
const MAX_SELECTED_USERS = 500

const users = ref<AdminUser[]>([])
const selectedSet = ref<Set<number>>(new Set())
const searchQuery = ref('')
const loadingUsers = ref(false)
const userLoadError = ref('')
const submitting = ref(false)
const groupId = ref<number | null>(null)
const validityDays = ref(30)
const assignmentResult = ref<BulkAssignSubscriptionResult | null>(null)
const pagination = reactive({ page: 1, total: 0, pages: 0 })
let searchTimeout: ReturnType<typeof setTimeout> | null = null
let usersAbortController: AbortController | null = null

const subscriptionGroupOptions = computed<GroupOption[]>(() =>
  props.groups
    .filter((group) => group.subscription_type === 'subscription' && group.status === 'active')
    .map((group) => ({
      value: group.id,
      label: group.name,
      description: group.description,
      platform: group.platform,
      subscriptionType: group.subscription_type,
      rate: group.rate_multiplier
    }))
)

const selectedIds = computed(() => Array.from(selectedSet.value))
const allCurrentPageSelected = computed(() =>
  users.value.length > 0 && users.value.every((user) => selectedSet.value.has(user.id))
)
const pageStart = computed(() => pagination.total === 0 ? 0 : (pagination.page - 1) * PAGE_SIZE + 1)
const pageEnd = computed(() => Math.min(pagination.page * PAGE_SIZE, pagination.total))
const selectionTooLarge = computed(() => selectedIds.value.length > MAX_SELECTED_USERS)
const hasValidValidity = computed(() =>
  Number.isInteger(validityDays.value) && validityDays.value >= 1 && validityDays.value <= 36500
)
const canSubmit = computed(() =>
  selectedIds.value.length > 0
  && !selectionTooLarge.value
  && groupId.value !== null
  && hasValidValidity.value
  && !submitting.value
)

const replaceSelection = (ids: number[]) => {
  selectedSet.value = new Set(ids)
}

const isSelected = (id: number) => selectedSet.value.has(id)

const toggleUser = (id: number) => {
  const next = new Set(selectedSet.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selectedSet.value = next
  assignmentResult.value = null
}

const toggleCurrentPage = () => {
  const next = new Set(selectedSet.value)
  users.value.forEach((user) => {
    if (allCurrentPageSelected.value) next.delete(user.id)
    else next.add(user.id)
  })
  selectedSet.value = next
  assignmentResult.value = null
}

const clearSelection = () => {
  replaceSelection([])
  assignmentResult.value = null
}

const loadUsers = async () => {
  usersAbortController?.abort()
  const controller = new AbortController()
  usersAbortController = controller
  loadingUsers.value = true
  userLoadError.value = ''
  try {
    const response = await adminAPI.users.list(
      pagination.page,
      PAGE_SIZE,
      { search: searchQuery.value.trim() || undefined },
      { signal: controller.signal }
    )
    if (controller.signal.aborted || usersAbortController !== controller) return
    users.value = response.items
    pagination.total = response.total
    pagination.pages = response.pages
  } catch (error: any) {
    if (controller.signal.aborted || error?.name === 'AbortError' || error?.code === 'ERR_CANCELED') return
    users.value = []
    userLoadError.value = error.response?.data?.detail || t('admin.subscriptions.bulkAssign.failedToLoadUsers')
  } finally {
    if (usersAbortController === controller) {
      loadingUsers.value = false
      usersAbortController = null
    }
  }
}

const handleSearchInput = () => {
  if (searchTimeout) clearTimeout(searchTimeout)
  searchTimeout = setTimeout(() => {
    pagination.page = 1
    void loadUsers()
  }, 300)
}

const goToPage = (page: number) => {
  if (page < 1 || page > pagination.pages || page === pagination.page) return
  pagination.page = page
  void loadUsers()
}

const reset = () => {
  if (searchTimeout) {
    clearTimeout(searchTimeout)
    searchTimeout = null
  }
  usersAbortController?.abort()
  usersAbortController = null
  users.value = []
  replaceSelection([])
  searchQuery.value = ''
  loadingUsers.value = false
  userLoadError.value = ''
  submitting.value = false
  groupId.value = null
  validityDays.value = 30
  assignmentResult.value = null
  pagination.page = 1
  pagination.total = 0
  pagination.pages = 0
}

const handleClose = () => {
  if (submitting.value) return
  emit('close')
}

const handleSubmit = async () => {
  if (!canSubmit.value || groupId.value === null) return

  const requestIds = [...new Set(selectedIds.value)]
  submitting.value = true
  assignmentResult.value = null
  try {
    const result = await adminAPI.subscriptions.bulkAssign({
      user_ids: requestIds,
      group_id: groupId.value,
      validity_days: validityDays.value
    })

    if (result.failed_count === 0) {
      appStore.showSuccess(t('admin.subscriptions.bulkAssign.success', {
        count: result.success_count,
        created: result.created_count,
        reused: result.reused_count
      }))
      emit('success', result)
      emit('close')
      return
    }

    assignmentResult.value = result
    if (result.success_count > 0) {
      const failedIds = requestIds.filter((id) => result.statuses?.[String(id)] === 'failed')
      if (failedIds.length === result.failed_count) {
        replaceSelection(failedIds)
      }
      appStore.showWarning(t('admin.subscriptions.bulkAssign.partialWarning', {
        success: result.success_count,
        failed: result.failed_count
      }))
      emit('success', result)
    } else {
      appStore.showError(t('admin.subscriptions.bulkAssign.allFailed', { count: result.failed_count }))
    }
  } catch (error: any) {
    appStore.showError(
      error.response?.data?.detail
      || error.response?.data?.message
      || t('admin.subscriptions.failedToAssign')
    )
  } finally {
    submitting.value = false
  }
}

watch(
  () => props.show,
  (show) => {
    if (show) {
      reset()
      void loadUsers()
    } else {
      if (searchTimeout) {
        clearTimeout(searchTimeout)
        searchTimeout = null
      }
      usersAbortController?.abort()
    }
  },
  { immediate: true }
)

onUnmounted(() => {
  if (searchTimeout) clearTimeout(searchTimeout)
  usersAbortController?.abort()
})
</script>
