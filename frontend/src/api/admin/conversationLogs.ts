import { apiClient } from '../client'
import type { PaginatedResponse, UsageRequestType } from '@/types'

export interface ConversationLog {
  id: number
  request_id: string
  response_id: string
  user_id: number
  user_email: string
  api_key_id: number
  api_key_name: string
  account_id: number
  account_name: string
  group_id: number | null
  group_name: string
  platform: string
  inbound_endpoint: string
  upstream_endpoint: string
  model: string
  requested_model: string
  upstream_model: string
  request_type: UsageRequestType
  stream: boolean
  openai_ws_mode: boolean
  status_code: number
  duration_ms: number | null
  first_token_ms: number | null
  input_tokens: number
  output_tokens: number
  cache_read_tokens: number
  cache_create_tokens: number
  request_hash: string
  request_body: string
  response_body: string
  request_truncated: boolean
  response_truncated: boolean
  queue_delay_ms: number | null
  created_at: string
  total_tokens: number
}

export interface ConversationLogQueryParams {
  page?: number
  page_size?: number
  q?: string
  user_id?: number
  api_key_id?: number
  account_id?: number
  group_id?: number
  platform?: string
  model?: string
  request_id?: string
  response_id?: string
  request_type?: UsageRequestType
  stream?: boolean
  start_date?: string
  end_date?: string
  timezone?: string
  sort_by?: string
  sort_order?: 'asc' | 'desc'
}

export async function list(
  params: ConversationLogQueryParams,
  options?: { signal?: AbortSignal }
): Promise<PaginatedResponse<ConversationLog>> {
  const { data } = await apiClient.get<PaginatedResponse<ConversationLog>>('/admin/conversation-logs', {
    params,
    signal: options?.signal
  })
  return data
}

export async function getById(id: number): Promise<ConversationLog> {
  const { data } = await apiClient.get<ConversationLog>(`/admin/conversation-logs/${id}`)
  return data
}

export const adminConversationLogsAPI = {
  list,
  getById
}

export default adminConversationLogsAPI
