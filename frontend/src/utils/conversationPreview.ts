import { buildConversationTimeline } from './conversationTimeline'

const snippetLimit = 180

export interface ConversationPreview {
  text: string
  messageCount: number
  operationCount: number
}

export function buildConversationPreview(requestBody: string, responseBody: string): ConversationPreview {
  const timeline = buildConversationTimeline(requestBody, responseBody)
  const messages = timeline.rounds.flatMap((round) => round.messages)
  const preferredMessage =
    messages.find((message) => message.role === 'user' && message.parts.length > 0) ||
    messages.find((message) => message.role === 'assistant' && message.parts.length > 0) ||
    messages.find((message) => message.parts.length > 0)
  const rawText = preferredMessage?.parts.map((part) => part.text).find((part) => !looksLikePayload(part)) || ''

  return {
    text: compactText(rawText),
    messageCount: timeline.messageCount,
    operationCount: timeline.operationCount
  }
}

function compactText(value: string): string {
  const compact = value.replace(/\s+/g, ' ').trim()
  if (compact.length <= snippetLimit) return compact
  return `${compact.slice(0, snippetLimit - 1).trimEnd()}…`
}

function looksLikePayload(value: string): boolean {
  const trimmed = value.trim()
  return trimmed.startsWith('{') || trimmed.startsWith('[') || trimmed.startsWith('data:') || trimmed.startsWith('event:')
}
