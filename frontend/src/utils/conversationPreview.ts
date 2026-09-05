import { buildConversationTimeline } from './conversationTimeline'

export interface ConversationPreview {
  text: string
  messageCount: number
  operationCount: number
}

export function buildConversationPreview(requestBody: string, responseBody: string): ConversationPreview {
  const timeline = buildConversationTimeline(requestBody, responseBody)
  const messages = timeline.rounds.flatMap((round) => round.messages)
  const newestMessages = [...messages].reverse()
  const rawText = previewTextFrom(newestMessages.filter((message) => message.role === 'user'))

  return {
    text: rawText.replace(/\s+/g, ' ').trim(),
    messageCount: timeline.messageCount,
    operationCount: timeline.operationCount
  }
}

function previewTextFrom(messages: ReturnType<typeof buildConversationTimeline>['rounds'][number]['messages']): string {
  for (const message of messages) {
    const text = message.parts.map((part) => part.text).join('\n')
    if (message.id === 'request-text' && looksLikePayload(text)) return ''
    if (text) return text
  }
  return ''
}

function looksLikePayload(value: string): boolean {
  const trimmed = value.trim()
  return trimmed.startsWith('{') || trimmed.startsWith('[') || trimmed.startsWith('data:') || trimmed.startsWith('event:')
}
