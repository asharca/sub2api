import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { AdminGroup, AdminUser, BulkAssignSubscriptionResult } from '@/types'
import BulkAssignSubscriptionModal from '../BulkAssignSubscriptionModal.vue'

const { listUsers, bulkAssign, showSuccess, showWarning, showError } = vi.hoisted(() => ({
  listUsers: vi.fn(),
  bulkAssign: vi.fn(),
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      list: listUsers
    },
    subscriptions: {
      bulkAssign
    }
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showWarning,
    showError
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

const BaseDialogStub = {
  props: ['show', 'title'],
  emits: ['close'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>'
}

const SelectStub = {
  name: 'SelectStub',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<div data-test="select-stub" />'
}

const createUser = (id: number): AdminUser => ({
  id,
  username: `user-${id}`,
  email: `user-${id}@example.com`,
  role: 'user',
  balance: 0,
  concurrency: 1,
  status: 'active',
  allowed_groups: [],
  balance_notify_enabled: false,
  balance_notify_threshold: null,
  balance_notify_extra_emails: [],
  created_at: '2026-07-29T00:00:00Z',
  updated_at: '2026-07-29T00:00:00Z',
  notes: ''
})

const subscriptionGroup = {
  id: 91,
  name: 'Codex Subscription',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  status: 'active',
  subscription_type: 'subscription'
} as AdminGroup

const page = (items: AdminUser[], currentPage: number, total: number, pages: number) => ({
  items,
  total,
  page: currentPage,
  page_size: 20,
  pages
})

const result = (
  overrides: Partial<BulkAssignSubscriptionResult> = {}
): BulkAssignSubscriptionResult => ({
  success_count: 0,
  created_count: 0,
  reused_count: 0,
  failed_count: 0,
  subscriptions: [],
  errors: [],
  statuses: {},
  ...overrides
})

const mountModal = async () => {
  const wrapper = mount(BulkAssignSubscriptionModal, {
    props: {
      show: true,
      groups: [subscriptionGroup]
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        GroupBadge: true,
        GroupOptionItem: true,
        Icon: true
      }
    }
  })
  await flushPromises()
  return wrapper
}

const chooseGroup = async (wrapper: Awaited<ReturnType<typeof mountModal>>) => {
  wrapper.findComponent(SelectStub).vm.$emit('update:modelValue', subscriptionGroup.id)
  await wrapper.vm.$nextTick()
}

describe('BulkAssignSubscriptionModal', () => {
  beforeEach(() => {
    listUsers.mockReset()
    bulkAssign.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()
    showError.mockReset()

    listUsers.mockResolvedValue(page([createUser(1), createUser(2)], 1, 2, 1))
  })

  it('loads the first user page when opened', async () => {
    const wrapper = await mountModal()

    expect(listUsers).toHaveBeenCalledWith(
      1,
      20,
      { search: undefined },
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.get('[data-test="user-row-1"]').text()).toContain('user-1@example.com')
    expect(wrapper.get('[data-test="user-row-2"]').text()).toContain('user-2@example.com')
    expect(wrapper.get('[data-test="selected-count"]').text()).toContain('"count":0')
  })

  it('selects and unselects every user on the current page', async () => {
    const wrapper = await mountModal()

    await wrapper.get('[data-test="toggle-current-page"]').trigger('click')

    expect(wrapper.get('[data-test="selected-count"]').text()).toContain('"count":2')
    expect((wrapper.get('[data-test="user-row-1"] input').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="user-row-2"] input').element as HTMLInputElement).checked).toBe(true)

    await wrapper.get('[data-test="toggle-current-page"]').trigger('click')

    expect(wrapper.get('[data-test="selected-count"]').text()).toContain('"count":0')
  })

  it('keeps selected user IDs while paging forward and back', async () => {
    listUsers.mockImplementation((currentPage: number) => Promise.resolve(
      currentPage === 1
        ? page([createUser(1), createUser(2)], 1, 3, 2)
        : page([createUser(3)], 2, 3, 2)
    ))
    const wrapper = await mountModal()

    await wrapper.get('[data-test="toggle-current-page"]').trigger('click')
    await wrapper.get('[data-test="next-page"]').trigger('click')
    await flushPromises()

    expect(listUsers).toHaveBeenLastCalledWith(
      2,
      20,
      { search: undefined },
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.get('[data-test="selected-count"]').text()).toContain('"count":2')

    await wrapper.get('[data-test="toggle-current-page"]').trigger('click')
    expect(wrapper.get('[data-test="selected-count"]').text()).toContain('"count":3')

    await wrapper.get('[data-test="previous-page"]').trigger('click')
    await flushPromises()

    expect((wrapper.get('[data-test="user-row-1"] input').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="user-row-2"] input').element as HTMLInputElement).checked).toBe(true)
  })

  it('de-duplicates IDs across pages and closes after a fully successful assignment', async () => {
    listUsers.mockImplementation((currentPage: number) => Promise.resolve(
      currentPage === 1
        ? page([createUser(1), createUser(2)], 1, 4, 2)
        : page([createUser(2), createUser(3)], 2, 4, 2)
    ))
    bulkAssign.mockResolvedValue(result({
      success_count: 3,
      created_count: 2,
      reused_count: 1,
      statuses: { 1: 'created', 2: 'reused', 3: 'created' }
    }))
    const wrapper = await mountModal()

    await wrapper.get('[data-test="toggle-current-page"]').trigger('click')
    await wrapper.get('[data-test="next-page"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="toggle-current-page"]').trigger('click')
    await chooseGroup(wrapper)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(bulkAssign).toHaveBeenCalledTimes(1)
    expect(bulkAssign).toHaveBeenCalledWith({
      user_ids: [1, 2, 3],
      group_id: 91,
      validity_days: 30
    })
    expect(showSuccess).toHaveBeenCalledOnce()
    expect(wrapper.emitted('success')).toEqual([[expect.objectContaining({ success_count: 3 })]])
    expect(wrapper.emitted('close')).toEqual([[]])
  })

  it('keeps only failed IDs selected after a partial success', async () => {
    listUsers.mockResolvedValue(page([createUser(1), createUser(2), createUser(3)], 1, 3, 1))
    bulkAssign.mockResolvedValue(result({
      success_count: 2,
      created_count: 1,
      reused_count: 1,
      failed_count: 1,
      errors: ['user 2 failed'],
      statuses: { 1: 'created', 2: 'failed', 3: 'reused' }
    }))
    const wrapper = await mountModal()

    await wrapper.get('[data-test="toggle-current-page"]').trigger('click')
    await chooseGroup(wrapper)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-test="selected-count"]').text()).toContain('"count":1')
    expect((wrapper.get('[data-test="user-row-1"] input').element as HTMLInputElement).checked).toBe(false)
    expect((wrapper.get('[data-test="user-row-2"] input').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="user-row-3"] input').element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.get('[data-test="assignment-result"]').text()).toContain('user 2 failed')
    expect(showWarning).toHaveBeenCalledOnce()
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('keeps the full selection when every assignment fails', async () => {
    bulkAssign.mockResolvedValue(result({
      failed_count: 2,
      errors: ['all failed'],
      statuses: { 1: 'failed', 2: 'failed' }
    }))
    const wrapper = await mountModal()

    await wrapper.get('[data-test="toggle-current-page"]').trigger('click')
    await chooseGroup(wrapper)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-test="selected-count"]').text()).toContain('"count":2')
    expect((wrapper.get('[data-test="user-row-1"] input').element as HTMLInputElement).checked).toBe(true)
    expect((wrapper.get('[data-test="user-row-2"] input').element as HTMLInputElement).checked).toBe(true)
    expect(showError).toHaveBeenCalledOnce()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('keeps the selection when the bulk assignment request rejects', async () => {
    bulkAssign.mockRejectedValue({ response: { data: { detail: 'network unavailable' } } })
    const wrapper = await mountModal()

    await wrapper.get('[data-test="toggle-current-page"]').trigger('click')
    await chooseGroup(wrapper)
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-test="selected-count"]').text()).toContain('"count":2')
    expect(showError).toHaveBeenCalledWith('network unavailable')
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
  })
})
