import { describe, expect, it } from 'vitest'
import { parsePayload } from '@/utils/conversationPayload'

describe('parsePayload', () => {
  it('parses plain JSON payloads', () => {
    const payload = parsePayload('{"messages":[{"role":"user","content":"hello"}]}')

    expect(payload.parsed).toBe(true)
    expect(payload.format).toBe('json')
    expect(payload.value).toEqual({
      messages: [{ role: 'user', content: 'hello' }]
    })
  })

  it('parses OpenAI response event streams into JSON events', () => {
    const payload = parsePayload([
      'event: response.created',
      'data: {"type":"response.created","response":{"id":"resp_1","status":"in_progress"}}',
      '',
      'event: response.output_text.delta',
      'data: {"type":"response.output_text.delta","delta":"hello"}',
      '',
      'data: [DONE]',
      ''
    ].join('\n'))

    expect(payload.parsed).toBe(true)
    expect(payload.format).toBe('sse')
    expect(payload.value).toEqual({
      format: 'text/event-stream',
      events: [
        {
          index: 1,
          event: 'response.created',
          data: {
            type: 'response.created',
            response: {
              id: 'resp_1',
              status: 'in_progress'
            }
          }
        },
        {
          index: 2,
          event: 'response.output_text.delta',
          data: {
            type: 'response.output_text.delta',
            delta: 'hello'
          }
        },
        {
          index: 3,
          data: {
            done: true
          }
        }
      ]
    })
  })

  it('leaves non JSON text as raw text', () => {
    const payload = parsePayload('plain upstream text')

    expect(payload.parsed).toBe(false)
    expect(payload.format).toBe('text')
    expect(payload.raw).toBe('plain upstream text')
  })
})
