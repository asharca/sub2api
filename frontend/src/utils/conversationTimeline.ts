import { parsePayload, type ParsedPayload, type ParsedSseEvent } from './conversationPayload'

export type ConversationSource = 'request' | 'response'
export type ConversationRole = 'system' | 'user' | 'assistant' | 'tool' | 'unknown'
export type ConversationOperationKind = 'call' | 'result'

export interface ConversationPart {
  kind: 'text' | 'json' | 'media'
  text: string
  label?: string
  url?: string
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
  const webSocketTimeline = buildWebSocketTimeline(request, response)
  if (webSocketTimeline) return webSocketTimeline
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

function buildWebSocketTimeline(request: ParsedPayload, response: ParsedPayload): ConversationTimeline | null {
  const requestTurns = extractConversationTurns(request, 'request')
  const responseTurns = extractConversationTurns(response, 'response')
  if (requestTurns.length === 0 && responseTurns.length === 0) return null

  const turns = new Map<number, { request?: unknown; response?: unknown }>()
  for (const turn of requestTurns) {
    turns.set(turn.index, { ...(turns.get(turn.index) || {}), request: turn.payload })
  }
  for (const turn of responseTurns) {
    turns.set(turn.index, { ...(turns.get(turn.index) || {}), response: turn.payload })
  }

  const rounds = [...turns.entries()]
    .sort(([left], [right]) => left - right)
    .map(([index, turn]) => ({
      id: `round-${index}`,
      index,
      messages: [
        ...(turn.request === undefined ? [] : extractRequestMessages(asJSONPayload(turn.request))),
        ...(turn.response === undefined ? [] : extractResponseMessages(asJSONPayload(turn.response)))
      ].map((message) => ({ ...message, id: `turn-${index}-${message.id}` }))
    }))
    .filter((round) => round.messages.length > 0)
  const messages = rounds.flatMap((round) => round.messages)
  linkOperationNames(messages)
  return {
    rounds,
    messageCount: messages.length,
    operationCount: messages.reduce((count, message) => count + message.operations.length, 0),
    structured: true
  }
}

function extractConversationTurns(payload: ParsedPayload, key: 'request' | 'response') {
  if (!payload.parsed || payload.format !== 'json') return []
  const turns = asArray(asRecord(payload.value)?.conversation_turns)
  return turns.flatMap((value, offset) => {
    const turn = asRecord(value)
    if (!turn || turn[key] === undefined) return []
    const index = Number(turn.turn)
    return [{ index: Number.isInteger(index) && index > 0 ? index : offset + 1, payload: turn[key] }]
  })
}

function asJSONPayload(value: unknown): ParsedPayload {
  return { parsed: true, format: 'json', value, raw: JSON.stringify(value) || '' }
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

  if (Array.isArray(payload.value)) {
    return extractSseMessages(payload.value.map((data, index) => ({ index: index + 1, data })))
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
    messages.push(...normalizeMessage(item, 'response', `response-output-${index + 1}`))
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
  if (events.some((event) => String(asRecord(event.data)?.type ?? event.event ?? '').startsWith('response.'))) {
    return extractResponsesStream(events)
  }
  const messages: ConversationMessage[] = []
  const textParts: string[] = []
  const reasoningParts: string[] = []
  const toolCalls = new Map<string, ConversationOperation>()
  const toolResults: ConversationOperation[] = []

  for (const event of events) {
    const data = asRecord(event.data)
    if (!data || data.done === true) continue

    const eventType = String(data.type ?? event.event ?? '')
    const choice = asRecord(asArray(data.choices)[0])
    const delta = choice ? asRecord(choice.delta) : undefined
    const text = firstString(
      asRecord(data.delta)?.text,
      asRecord(data.content_block)?.text,
      delta?.content
    )
    if (text) textParts.push(text)
    const reasoning = firstString(delta?.reasoning_content, delta?.reasoning, asRecord(data.delta)?.thinking)
    if (reasoning) reasoningParts.push(reasoning)

    const nestedOperation = data.item ?? data.content_block ?? data.output_item
    const operation = normalizeOperation(nestedOperation ?? data, `sse-operation-${event.index}`)
    if (operation?.kind === 'result') {
      toolResults.push(operation)
    } else if (operation) {
      const key = data.index !== undefined ? `block-${data.index}` : operation.callId || operation.id
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
        input: functionValue?.arguments
      }
      const key = `choice-${choice?.index ?? 0}-tool-${call?.index ?? index}`
      const existing = toolCalls.get(key)
      toolCalls.set(key, existing ? mergeOperation(existing, operation) : operation)
    })

    if (eventType.includes('arguments.delta') || asRecord(data.delta)?.type === 'input_json_delta') {
      const partial = firstString(data.delta, asRecord(data.delta)?.partial_json)
      if (partial) {
        const key = data.index !== undefined ? `block-${data.index}` : firstString(data.item_id, data.call_id) || `sse-operation-${event.index}`
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

  if (textParts.length > 0 || reasoningParts.length > 0 || toolCalls.size > 0 || toolResults.length > 0) {
    const parts: ConversationPart[] = []
    if (reasoningParts.length) parts.push({ kind: 'text', label: 'reasoning', text: reasoningParts.join('') })
    if (textParts.length) parts.push({ kind: 'text', text: textParts.join('') })
    messages.push(createMessage('response', 'assistant', parts, [
      ...[...toolCalls.values()].map((operation) => ({ ...operation, input: parseMaybeJson(operation.input) })),
      ...toolResults
    ], 'response-stream'))
  }

  return messages
}

// Deltas update an output item; done/completed snapshots replace it, never append it twice.
function extractResponsesStream(events: ParsedSseEvent[]): ConversationMessage[] {
  const items = new Map<string, JsonRecord>()
  const errors: ConversationMessage[] = []
  for (const event of events) {
    const data = asRecord(event.data)
    if (!data) continue
    const type = String(data.type ?? event.event ?? '')
    const response = asRecord(data.response)
    if (response && Array.isArray(response.output) && response.output.length) {
      items.clear()
      response.output.forEach((value, index) => {
        const item = asRecord(value)
        if (item) items.set(String(item.id ?? index), item)
      })
    }
    const snapshot = asRecord(data.item)
    const key = String(snapshot?.id ?? data.item_id ?? data.output_index ?? 0)
    if (snapshot && (type === 'response.output_item.added' || type === 'response.output_item.done')) {
      items.set(key, snapshot)
    } else if (type.includes('function_call_arguments.') || type.includes('custom_tool_call_input.')) {
      const field = type.includes('custom_tool') ? 'input' : 'arguments'
      const item = items.get(key) || { type: field === 'input' ? 'custom_tool_call' : 'function_call' }
      if (data.name) item.name = data.name
      if (data.call_id) item.call_id = data.call_id
      item[field] = type.endsWith('.delta') ? String(item[field] ?? '') + String(data.delta ?? '') : data[field] ?? item[field]
      items.set(key, item)
    } else if (/^response\.(output_text|refusal|reasoning_summary_text)\.(delta|done)$/.test(type)) {
      const reasoning = type.includes('reasoning_summary')
      const field = reasoning ? 'summary' : 'content'
      const textField = type.includes('refusal') ? 'refusal' : 'text'
      const item = items.get(key) || { type: reasoning ? 'reasoning' : 'message', role: 'assistant' }
      const parts = [...asArray(item[field])]
      const index = Number(data.content_index ?? data.summary_index ?? 0)
      const part = { ...asRecord(parts[index]), type: reasoning ? 'summary_text' : textField === 'refusal' ? 'refusal' : 'output_text' }
      const content: JsonRecord = part
      content[textField] = type.endsWith('.delta') ? String(content[textField] ?? '') + String(data.delta ?? '') : data[textField] ?? content[textField]
      parts[index] = content
      item[field] = parts
      items.set(key, item)
    }
    const error = data.error ?? response?.error ?? (type === 'error' ? data : undefined)
    if (error) errors.push(createMessage('response', 'assistant', [{ kind: 'json', label: 'error', text: formatValue(error) }], [], `response-error-${event.index}`))
  }
  return [...[...items.values()].flatMap((item, index) => normalizeMessage(item, 'response', `response-output-${index}`)), ...errors]
}

function normalizeMessage(value: unknown, source: ConversationSource, id: string): ConversationMessage[] {
  const record = asRecord(value)
  if (!record) {
    const text = stringifyContent(value)
    return text ? [createMessage(source, source === 'request' ? 'user' : 'assistant', [{ kind: 'text', text }], [], id)] : []
  }

  const itemType = String(record.type ?? '')
  const role = normalizeRole(firstString(record.role, record.author_role, record.author,
    itemType.endsWith('_call_output') ? 'tool' : itemType.endsWith('_call') || itemType === 'reasoning' ? 'assistant' : source === 'response' ? 'assistant' : itemType))
  const operations = collectOperations(record, id)
  const parts = role === 'tool' && operations.length > 0
    ? []
    : contentToParts(record.content ?? record.parts ?? record.text ?? record.output_text)
  if (record.thinking !== undefined) {
    parts.push(...contentToParts(record.thinking))
  }
  if (itemType === 'reasoning') {
    parts.push(...contentToParts(record.summary).map((part) => ({ ...part, label: 'reasoning' })))
    if (!parts.length && record.encrypted_content) parts.push({ kind: 'media', label: 'reasoning', text: '[encrypted_content]' })
  }
  for (const reasoning of [record.reasoning_content, record.reasoning]) {
    if (typeof reasoning === 'string') parts.unshift({ kind: 'text', label: 'reasoning', text: reasoning })
  }
  if (typeof record.refusal === 'string') parts.push({ kind: 'text', label: 'refusal', text: record.refusal })

  if (parts.length === 0 && operations.length === 0) {
    const fallback = pickMessageText(record)
    if (fallback) parts.push({ kind: 'text', text: fallback })
  }

  if (parts.length === 0 && operations.length === 0 && itemType && itemType !== 'message') {
    parts.push({ kind: 'json', label: itemType, text: formatValue(record) })
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
    type.endsWith('_output') ||
    type === 'function_response' ||
    record.functionResponse !== undefined ||
    record.role === 'tool' ||
    record.tool_call_id !== undefined ||
    record.call_id !== undefined && type.includes('output')
  const looksLikeCall =
    type.includes('tool') || type.endsWith('_call') || type.endsWith('_call_output') ||
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
    callId: firstString(record.call_id, record.tool_call_id, record.tool_use_id, record.id, functionCall?.call_id),
    input: isResult ? undefined : parseMaybeJson(input ?? record.action),
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
    return [{ kind: 'media', label: 'image', text: '[image]', url: firstString(asRecord(record.image_url)?.url, record.image_url) }]
  }
  if (type === 'thinking') return [{ kind: 'text', label: 'reasoning', text: firstString(record.thinking) }]
  if (type === 'refusal') return [{ kind: 'text', label: 'refusal', text: firstString(record.refusal) }]
  if (typeof record.text === 'string') return [{ kind: 'text', text: record.text }]
  if (typeof record.input_text === 'string') return [{ kind: 'text', text: record.input_text }]
  if (typeof record.output_text === 'string') return [{ kind: 'text', text: record.output_text }]
  if (record.parts !== undefined) return contentToParts(record.parts)
  if (record.content !== undefined && record.content !== value) return contentToParts(record.content)
  return [{ kind: 'json', label: type || 'content', text: formatValue(record) }]
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
  const existing = typeof current === 'string' ? current : ''
  return existing + partial
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
