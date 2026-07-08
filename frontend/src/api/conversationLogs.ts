import { apiClient } from './client'
import type { PaginatedResponse } from '@/types'
import type { ConversationLog, ConversationLogQueryParams } from '@/api/admin/conversationLogs'

export async function list(
  params: ConversationLogQueryParams,
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<ConversationLog>> {
  const { data } = await apiClient.get<PaginatedResponse<ConversationLog>>('/conversation-logs', {
    params,
    signal: options?.signal
  })
  return data
}

export async function getById(id: number): Promise<ConversationLog> {
  const { data } = await apiClient.get<ConversationLog>(`/conversation-logs/${id}`)
  return data
}

export const conversationLogsAPI = {
  list,
  getById
}

export default conversationLogsAPI
