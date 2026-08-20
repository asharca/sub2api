import { getLocale } from '@/i18n'
import { buildApiUrl } from './client'
import type { ConversationLog, ConversationLogQueryParams } from './admin/conversationLogs'

export async function streamConversationLogs(
  path: string,
  params: ConversationLogQueryParams,
  options: {
    signal: AbortSignal
    onOpen: () => void
    onLog: (log: ConversationLog) => void
  }
): Promise<void> {
  const url = new URL(buildApiUrl(path), window.location.origin)
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== null && value !== '') url.searchParams.set(key, String(value))
  }
  url.searchParams.set('timezone', Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC')

  const headers = new Headers({
    Accept: 'text/event-stream',
    'Accept-Language': getLocale()
  })
  const token = localStorage.getItem('auth_token')
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const response = await fetch(url, { headers, credentials: 'include', signal: options.signal })
  if (!response.ok || !response.body) throw new Error(`Conversation stream failed: ${response.status}`)
  options.onOpen()

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    while (true) {
      const { done, value } = await reader.read()
      if (done) return
      buffer += decoder.decode(value, { stream: true })
      const events = buffer.split(/\r?\n\r?\n/)
      buffer = events.pop() || ''
      for (const event of events) {
        const type = event.match(/^event:\s*(.+)$/m)?.[1]
        if (type !== 'conversation_log') continue
        const data = event.split(/\r?\n/).filter((line) => line.startsWith('data:')).map((line) => line.slice(5).trimStart()).join('\n')
        if (!data) continue
        options.onLog(JSON.parse(data) as ConversationLog)
      }
    }
  } finally {
    reader.releaseLock()
  }
}
