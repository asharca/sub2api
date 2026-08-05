import { parsePayload, type ParsedPayload, type ParsedSseEvent } from './conversationPayload'

export type ConversationSource = 'request' | 'response'
export type ConversationRole = 'system' | 'user' | 'assistant' | 'tool' | 'unknown'
export type ConversationOperationKind = 'call' | 'result'

export interface ConversationPart {
  kind: 'text' | 'json' | 'media'
  text: string
  label?: string
}

export interface ConversationOperation {
  id: string
  kind: ConversationOperationKind
  name: string
  callId?: string
  input?: unknown
  output?: unknown
}

export interface ConversationMessage {
  id: string
  role: ConversationRole
  source: ConversationSource
  parts: ConversationPart[]
  operations: ConversationOperation[]
}

export interface ConversationRound {
  id: string
  index: number
  messages: ConversationMessage[]
}

export interface ConversationTimeline {
  rounds: ConversationRound[]
  operationCount: number
  messageCount: number
  structured: boolean
}

type JsonRecord = Record<string, unknown>

const ROLE_ALIASES: Record<string, ConversationRole> = {
  model: 'assistant',
  bot: 'assistant',
  function: 'tool',
  tool: 'tool',
  developer: 'system'
}

export function buildConversationTimeline(requestBody: string, responseBody: string): ConversationTimeline {
  const request = parsePayload(requestBody)
  const response = parsePayload(responseBody)
  const messages = [
    ...extractRequestMessages(request),
    ...extractResponseMessages(response)
  ]
  linkOperationNames(messages)

  const rounds: ConversationRound[] = []
  let round: ConversationRound | null = null
  let nextRound = 1

  for (const message of messages) {
    const startsRound =
      message.role === 'user' &&
      !message.operations.some((operation) => operation.kind === 'result') &&
      round !== null &&
      round.messages.some((item) => item.role === 'user')
    if (!round || startsRound) {
      round = {
        id: `round-${nextRound}`,
        index: nextRound,
        messages: []
      }
      rounds.push(round)
      nextRound += 1
    }
    round.messages.push(message)
  }

  return {
    rounds,
    messageCount: messages.length,
    operationCount: messages.reduce((count, message) => count + message.operations.length, 0),
    structured: request.parsed || response.parsed
  }
}

function linkOperationNames(messages: ConversationMessage[]) {
  const namesByCallId = new Map<string, string>()
  for (const message of messages) {
    for (const operation of message.operations) {
      if (operation.kind === 'call' && operation.callId) {
        namesByCallId.set(operation.callId, operation.name)
      }
      if (operation.kind === 'result' && operation.callId && namesByCallId.has(operation.callId)) {
        operation.name = namesByCallId.get(operation.callId) || operation.name
      }
    }
  }
}

function extractRequestMessages(payload: ParsedPayload): ConversationMessage[] {
  if (!payload.parsed) {
    return payload.raw ? [createMessage('request', 'user', [{ kind: 'text', text: payload.raw }], [], 'request-text')] : []
  }

  if (payload.format === 'sse') {
    return []
  }

  const value = asRecord(payload.value)
  if (!value) return []

  const contextMessages: ConversationMessage[] = []
  const system = value.system ?? value.instructions ?? value.systemInstruction
  if (system !== undefined) {
    contextMessages.push(...normalizeMessage({ role: 'system', content: system }, 'request', 'request-system'))
  }

  const messages = asArray(value.messages)
  if (messages.length > 0) {
    return [
      ...contextMessages,
      ...messages.flatMap((item, index) => normalizeMessage(item, 'request', `request-message-${index + 1}`))
    ]
  }

  const input = value.input
  if (Array.isArray(input)) {
    return [
      ...contextMessages,
      ...input.flatMap((item, index) => normalizeMessage(item, 'request', `request-input-${index + 1}`))
    ]
  }
  if (input !== undefined) {
    return [
      ...contextMessages,
      ...normalizeMessage({ role: 'user', content: input }, 'request', 'request-input-1')
    ]
  }

  const contents = asArray(value.contents)
  if (contents.length > 0) {
    return [
      ...contextMessages,
      ...contents.flatMap((item, index) => normalizeGeminiContent(item, 'request', `request-content-${index + 1}`))
    ]
  }

  const prompt = firstString(value.prompt, value.text)
  return prompt
    ? [...contextMessages, createMessage('request', 'user', [{ kind: 'text', text: prompt }], [], 'request-prompt')]
    : contextMessages
}

function extractResponseMessages(payload: ParsedPayload): ConversationMessage[] {
  if (!payload.parsed) {
    return payload.raw ? [createMessage('response', 'assistant', [{ kind: 'text', text: payload.raw }], [], 'response-text')] : []
  }

  if (payload.format === 'sse') {
    return extractSseMessages(payload.value.events)
  }

  const value = asRecord(payload.value)
  if (!value) return []

  const messages: ConversationMessage[] = []
  const choices = asArray(value.choices)
  choices.forEach((choice, index) => {
    const record = asRecord(choice)
    const message = record && (record.message ?? record.delta)
    if (message !== undefined) {
      messages.push(...normalizeMessage(message, 'response', `response-choice-${index + 1}`))
    }
  })

  const candidates = asArray(value.candidates)
  candidates.forEach((candidate, index) => {
    const content = asRecord(candidate)?.content
    if (content !== undefined) {
      messages.push(...normalizeGeminiContent(content, 'response', `response-candidate-${index + 1}`))
    }
  })

  const output = asArray(value.output)
  output.forEach((item, index) => {
    const record = asRecord(item)
    if (!record) return
    if (record.type === 'message' || record.role) {
      messages.push(...normalizeMessage(record, 'response', `response-output-${index + 1}`))
      return
    }
    const operation = normalizeOperation(record, `response-output-operation-${index + 1}`)
    if (operation) {
      messages.push(createMessage('response', 'assistant', [], [operation], `response-output-${index + 1}`))
    }
  })

  if (messages.length === 0) {
    const directContent = value.content
    if (directContent !== undefined) {
      messages.push(...normalizeMessage({ role: 'assistant', content: directContent }, 'response', 'response-content'))
    }
  }

  if (messages.length === 0) {
    const text = firstString(value.output_text, value.text)
    if (text) messages.push(createMessage('response', 'assistant', [{ kind: 'text', text }], [], 'response-text'))
  }

  const error = asRecord(value.error)
  if (error) {
    messages.push(createMessage('response', 'assistant', [{ kind: 'text', text: formatValue(error) }], [], 'response-error'))
  }

  return messages
}

function extractSseMessages(events: ParsedSseEvent[]): ConversationMessage[] {
  const messages: ConversationMessage[] = []
  const textParts: string[] = []
  const toolCalls = new Map<string, ConversationOperation>()
  const toolResults: ConversationOperation[] = []

  for (const event of events) {
    const data = asRecord(event.data)
    if (!data || data.done === true) continue

    const eventType = String(data.type ?? event.event ?? '')
    const choice = asRecord(asArray(data.choices)[0])
    const delta = choice ? asRecord(choice.delta) : undefined
    const text = firstString(
      data.delta,
      data.text,
      asRecord(data.content_block)?.text,
      delta?.content
    )
    if (text) textParts.push(text)

    const nestedOperation = data.item ?? data.content_block ?? data.output_item
    const operation = normalizeOperation(nestedOperation ?? data, `sse-operation-${event.index}`)
    if (operation?.kind === 'result') {
      toolResults.push(operation)
    } else if (operation) {
      const key = operation.callId || operation.id
      const existing = toolCalls.get(key)
      toolCalls.set(key, existing ? mergeOperation(existing, operation) : operation)
    }

    const deltaToolCalls = asArray(delta?.tool_calls)
    deltaToolCalls.forEach((item, index) => {
      const call = asRecord(item)
      const functionValue = asRecord(call?.function)
      const operation: ConversationOperation = {
        id: `sse-tool-${call?.index ?? index}`,
        kind: 'call',
        name: firstString(functionValue?.name, call?.name) || 'function',
        callId: firstString(call?.id),
        input: parseMaybeJson(functionValue?.arguments)
      }
      const key = operation.callId || operation.id
      const existing = toolCalls.get(key)
      toolCalls.set(key, existing ? mergeOperation(existing, operation) : operation)
    })

    if (eventType.includes('arguments.delta') || eventType.includes('input_json_delta')) {
      const partial = firstString(data.delta, asRecord(data.delta)?.partial_json)
      if (partial) {
        const key = firstString(data.item_id, data.call_id, data.output_index) || `sse-operation-${event.index}`
        const current = toolCalls.get(key) || {
          id: key,
          kind: 'call' as const,
          name: firstString(data.name) || 'function',
          callId: firstString(data.call_id)
        }
        current.input = appendPartialJson(current.input, partial)
        toolCalls.set(key, current)
      }
    }
  }

  if (textParts.length > 0 || toolCalls.size > 0) {
    messages.push(createMessage('response', 'assistant', textParts.length ? [{ kind: 'text', text: textParts.join('') }] : [], [
      ...toolCalls.values(),
      ...toolResults
    ], 'response-stream'))
  }

  return messages
}

function normalizeMessage(value: unknown, source: ConversationSource, id: string): ConversationMessage[] {
  const record = asRecord(value)
  if (!record) {
    const text = stringifyContent(value)
    return text ? [createMessage(source, source === 'request' ? 'user' : 'assistant', [{ kind: 'text', text }], [], id)] : []
  }

  const role = normalizeRole(firstString(record.role, record.author_role, record.type))
  const operations = collectOperations(record, id)
  const parts = role === 'tool' && operations.length > 0
    ? []
    : contentToParts(record.content ?? record.parts ?? record.text ?? record.output_text)
  if (record.thinking !== undefined) {
    parts.push(...contentToParts(record.thinking))
  }

  if (parts.length === 0 && operations.length === 0) {
    const fallback = pickMessageText(record)
    if (fallback) parts.push({ kind: 'text', text: fallback })
  }

  if (parts.length === 0 && operations.length === 0) return []
  return [createMessage(source, role, parts, operations, id)]
}

function normalizeGeminiContent(value: unknown, source: ConversationSource, id: string): ConversationMessage[] {
  const record = asRecord(value)
  if (!record) return []
  const role = normalizeRole(firstString(record.role, record.author))
  const parts = asArray(record.parts)
  const operations = parts.flatMap((part, index) => collectOperations(asRecord(part) || {}, `${id}-operation-${index + 1}`))
  const normalizedParts = contentToParts(parts)
  if (normalizedParts.length === 0 && operations.length === 0) return []
  return [createMessage(source, role, normalizedParts, operations, id)]
}

function createMessage(
  source: ConversationSource,
  role: ConversationRole,
  parts: ConversationPart[],
  operations: ConversationOperation[],
  id: string
): ConversationMessage {
  return { id, source, role, parts, operations }
}

function collectOperations(record: JsonRecord, id: string): ConversationOperation[] {
  const operations: ConversationOperation[] = []
  const toolCalls = asArray(record.tool_calls)
  toolCalls.forEach((call, index) => {
    const operation = normalizeOperation(call, `${id}-tool-call-${index + 1}`)
    if (operation) operations.push(operation)
  })

  const directOperation = normalizeOperation(record, `${id}-operation`)
  if (directOperation) operations.push(directOperation)

  const content = asArray(record.content)
  content.forEach((part, index) => {
    const operation = normalizeOperation(part, `${id}-content-operation-${index + 1}`)
    if (operation) operations.push(operation)
  })
  return dedupeOperations(operations)
}

function normalizeOperation(value: unknown, id: string): ConversationOperation | null {
  const record = asRecord(value)
  if (!record) return null
  const type = String(record.type ?? '').toLowerCase()
  const functionValue = asRecord(record.function)
  const isResult =
    type.includes('result') ||
    type.includes('output') ||
    type === 'function_response' ||
    record.functionResponse !== undefined ||
    record.role === 'tool' ||
    record.tool_call_id !== undefined ||
    record.call_id !== undefined && type.includes('output')
  const looksLikeCall =
    type.includes('tool') ||
    type.includes('function') ||
    record.function_call !== undefined ||
    record.functionCall !== undefined ||
    record.functionResponse !== undefined ||
    record.tool_use_id !== undefined ||
    record.tool_call_id !== undefined ||
    record.role === 'tool'

  if (!looksLikeCall) return null

  const functionCall = asRecord(record.function_call) || asRecord(record.functionCall)
  const functionResponse = asRecord(record.functionResponse)
  const name = firstString(
    record.name,
    functionValue?.name,
    functionCall?.name,
    functionResponse?.name,
    type.replace(/^(response\.|content_block\.)/, '').replace(/[._-]/g, ' ')
  ) || (isResult ? 'tool result' : 'function')
  const input = record.input ?? functionValue?.arguments ?? functionCall?.arguments ?? functionCall?.args ?? record.arguments ?? record.args
  const output = record.output ?? record.content ?? record.response ?? functionResponse?.response ?? record.result

  return {
    id,
    kind: isResult ? 'result' : 'call',
    name,
    callId: firstString(record.call_id, record.tool_call_id, record.id, functionCall?.call_id),
    input: isResult ? undefined : parseMaybeJson(input),
    output: isResult ? parseMaybeJson(output) : undefined
  }
}

function contentToParts(value: unknown): ConversationPart[] {
  if (value === undefined || value === null) return []
  if (typeof value === 'string') return value ? [{ kind: 'text', text: value }] : []
  if (Array.isArray(value)) return value.flatMap((item) => contentToParts(item))

  const record = asRecord(value)
  if (!record) return [{ kind: 'text', text: String(value) }]
  const type = String(record.type ?? '').toLowerCase()
  if (type.includes('tool') || type.includes('function') || type.includes('result')) return []
  if (type.includes('image') || record.inlineData !== undefined || record.image_url !== undefined) {
    return [{ kind: 'media', label: 'image', text: '[image]' }]
  }
  if (typeof record.text === 'string') return [{ kind: 'text', text: record.text }]
  if (typeof record.input_text === 'string') return [{ kind: 'text', text: record.input_text }]
  if (typeof record.output_text === 'string') return [{ kind: 'text', text: record.output_text }]
  if (record.parts !== undefined) return contentToParts(record.parts)
  if (record.content !== undefined && record.content !== value) return contentToParts(record.content)
  return []
}

function pickMessageText(record: JsonRecord): string {
  return firstString(record.text, record.input, record.output, record.message)
}

function normalizeRole(value: string): ConversationRole {
  const normalized = value.trim().toLowerCase()
  if (normalized === 'system' || normalized === 'user' || normalized === 'assistant' || normalized === 'tool') return normalized
  return ROLE_ALIASES[normalized] || (normalized.includes('assistant') || normalized.includes('model') ? 'assistant' : 'unknown')
}

function dedupeOperations(operations: ConversationOperation[]): ConversationOperation[] {
  const seen = new Set<string>()
  return operations.filter((operation) => {
    const key = `${operation.kind}:${operation.callId || operation.name}:${formatValue(operation.input)}:${formatValue(operation.output)}`
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
}

function mergeOperation(base: ConversationOperation, next: ConversationOperation): ConversationOperation {
  return {
    ...base,
    name: next.name !== 'function' ? next.name : base.name,
    callId: next.callId || base.callId,
    input: mergeOperationValue(base.input, next.input),
    output: next.output ?? base.output
  }
}

function mergeOperationValue(base: unknown, next: unknown): unknown {
  if (typeof base === 'string' && typeof next === 'string') return base + next
  return next ?? base
}

function appendPartialJson(current: unknown, partial: string): unknown {
  const existing = typeof current === 'string' ? current : current ? formatValue(current) : ''
  return parseMaybeJson(existing + partial)
}

function parseMaybeJson(value: unknown): unknown {
  if (typeof value !== 'string') return value
  const trimmed = value.trim()
  if (!trimmed) return value
  try {
    return JSON.parse(trimmed)
  } catch {
    return value
  }
}

function asRecord(value: unknown): JsonRecord | null {
  return typeof value === 'object' && value !== null && !Array.isArray(value) ? value as JsonRecord : null
}

function asArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : []
}

function firstString(...values: unknown[]): string {
  for (const value of values) {
    if (typeof value === 'string' && value.trim()) return value
  }
  return ''
}

function stringifyContent(value: unknown): string {
  if (typeof value === 'string') return value
  return value === undefined ? '' : formatValue(value)
}

export function formatValue(value: unknown): string {
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2) || String(value)
  } catch {
    return String(value)
  }
}
