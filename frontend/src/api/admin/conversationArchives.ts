import { apiClient } from '../client'
import type { ConversationLogQueryParams } from './conversationLogs'

export interface ConversationArchive {
  id: string
  status: 'running' | 'completed' | 'failed' | 'cleanup_failed'
  file_name: string
  size_bytes: number
  sha256: string
  record_count: number
  deleted_count: number
  delete_after_backup: boolean
  filters: Record<string, unknown>
  started_at: string
  finished_at?: string
  error_message?: string
}

const base = '/admin/conversation-logs/archives'
export const conversationArchivesAPI = {
  async list() {
    const { data } = await apiClient.get<{ items: ConversationArchive[] }>(base)
    return data.items || []
  },
  async create(params: ConversationLogQueryParams, deleteAfterBackup: boolean) {
    const { data } = await apiClient.post<ConversationArchive>(base, { delete_after_backup: deleteAfterBackup }, { params })
    return data
  },
  async download(id: string) {
    const { data } = await apiClient.get<{ url: string }>(`${base}/${encodeURIComponent(id)}/download`)
    return data.url
  }
}
