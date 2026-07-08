export type ParsedPayload =
  | {
      parsed: false
      format: 'text'
      value: null
      raw: string
    }
  | {
      parsed: true
      format: 'json'
      value: unknown
      raw: string
    }
  | {
      parsed: true
      format: 'sse'
      value: ParsedSsePayload
      raw: string
    }

export interface ParsedSsePayload {
  format: 'text/event-stream'
  events: ParsedSseEvent[]
}

export interface ParsedSseEvent {
  index: number
  event?: string
  id?: string
  retry?: number
  data?: unknown
  comments?: string[]
}

interface SseEventDraft {
  event?: string
  id?: string
  retry?: number
  dataLines: string[]
  comments: string[]
  hasFields: boolean
}

export function parsePayload(body: string): ParsedPayload {
  if (!body) return { parsed: false, format: 'text', value: null, raw: '' }
  const trimmed = body.trim()
  if (!trimmed) return { parsed: false, format: 'text', value: null, raw: '' }

  const sse = parseSsePayload(body)
  if (sse) {
    return { parsed: true, format: 'sse', value: sse, raw: body }
  }

  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) {
    return { parsed: false, format: 'text', value: null, raw: body }
  }
  try {
    return { parsed: true, format: 'json', value: JSON.parse(trimmed), raw: body }
  } catch {
    return { parsed: false, format: 'text', value: null, raw: body }
  }
}

function parseSsePayload(body: string): ParsedSsePayload | null {
  if (!looksLikeSse(body)) return null

  const events: ParsedSseEvent[] = []
  let draft = createSseDraft()
  const lines = normalizeNewlines(body).split('\n')

  for (const line of lines) {
    if (line === '') {
      flushSseEvent(events, draft)
      draft = createSseDraft()
      continue
    }

    draft.hasFields = true
    if (line.startsWith(':')) {
      draft.comments.push(line.slice(1).replace(/^ /, ''))
      continue
    }

    const separatorIndex = line.indexOf(':')
    const field = separatorIndex >= 0 ? line.slice(0, separatorIndex) : line
    const value = separatorIndex >= 0 ? line.slice(separatorIndex + 1).replace(/^ /, '') : ''

    switch (field) {
      case 'event':
        draft.event = value
        break
      case 'data':
        draft.dataLines.push(value)
        break
      case 'id':
        draft.id = value
        break
      case 'retry': {
        const retry = Number(value)
        if (Number.isFinite(retry)) draft.retry = retry
        break
      }
      default:
        break
    }
  }

  flushSseEvent(events, draft)
  if (events.length === 0) return null

  return {
    format: 'text/event-stream',
    events
  }
}

function looksLikeSse(body: string): boolean {
  const firstLine = normalizeNewlines(body)
    .split('\n')
    .find((line) => line.trim().length > 0)
    ?.trimStart()

  if (!firstLine) return false
  return (
    firstLine.startsWith('event:') ||
    firstLine.startsWith('data:') ||
    firstLine.startsWith('id:') ||
    firstLine.startsWith('retry:')
  )
}

function createSseDraft(): SseEventDraft {
  return {
    dataLines: [],
    comments: [],
    hasFields: false
  }
}

function flushSseEvent(events: ParsedSseEvent[], draft: SseEventDraft) {
  if (!draft.hasFields) return

  const event: ParsedSseEvent = {
    index: events.length + 1
  }

  if (draft.event) event.event = draft.event
  if (draft.id) event.id = draft.id
  if (draft.retry !== undefined) event.retry = draft.retry
  if (draft.comments.length > 0) event.comments = draft.comments
  if (draft.dataLines.length > 0) {
    event.data = parseSseData(draft.dataLines.join('\n'))
  }

  events.push(event)
}

function parseSseData(data: string): unknown {
  const trimmed = data.trim()
  if (trimmed === '[DONE]') {
    return { done: true }
  }
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) {
    return data
  }
  try {
    return JSON.parse(trimmed)
  } catch {
    return data
  }
}

function normalizeNewlines(value: string): string {
  return value.replace(/\r\n/g, '\n').replace(/\r/g, '\n')
}
