import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({
  post: vi.fn()
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    post
  }
}))

import { bulkAssign } from '@/api/admin/subscriptions'
import type {
  BulkAssignSubscriptionRequest,
  BulkAssignSubscriptionResult
} from '@/types'

type Assert<T extends true> = T
type IsExact<T, U> = (
  (<G>() => G extends T ? 1 : 2) extends (<G>() => G extends U ? 1 : 2)
    ? ((<G>() => G extends U ? 1 : 2) extends (<G>() => G extends T ? 1 : 2) ? true : false)
    : false
)

type ExpectedRequest = {
  user_ids: number[]
  group_id: number
  validity_days?: number
}

type ExpectedResult = {
  success_count: number
  created_count: number
  reused_count: number
  failed_count: number
  subscriptions: import('@/types').UserSubscription[]
  errors: string[]
  statuses?: Record<string, import('@/types').BulkAssignSubscriptionStatus>
}

const requestTypeIsExact: Assert<IsExact<BulkAssignSubscriptionRequest, ExpectedRequest>> = true
const resultTypeIsExact: Assert<IsExact<BulkAssignSubscriptionResult, ExpectedResult>> = true
const apiReturnTypeIsExact: Assert<
  IsExact<Awaited<ReturnType<typeof bulkAssign>>, BulkAssignSubscriptionResult>
> = true

describe('admin subscriptions API', () => {
  beforeEach(() => {
    post.mockReset()
  })

  it('posts one bulk request and returns the result object without reshaping it', async () => {
    const request: BulkAssignSubscriptionRequest = {
      user_ids: [4, 7, 11],
      group_id: 91,
      validity_days: 45
    }
    const response: BulkAssignSubscriptionResult = {
      success_count: 2,
      created_count: 1,
      reused_count: 1,
      failed_count: 1,
      subscriptions: [],
      errors: ['user 11 failed'],
      statuses: {
        4: 'created',
        7: 'reused',
        11: 'failed'
      }
    }
    post.mockResolvedValue({ data: response })

    const actual = await bulkAssign(request)

    expect(post).toHaveBeenCalledTimes(1)
    expect(post).toHaveBeenCalledWith('/admin/subscriptions/bulk-assign', request)
    expect(actual).toBe(response)
    expect(Array.isArray(actual)).toBe(false)
    expect(requestTypeIsExact).toBe(true)
    expect(resultTypeIsExact).toBe(true)
    expect(apiReturnTypeIsExact).toBe(true)
  })
})
