import { describe, expect, it } from 'vitest'
import { buildConversationTimeline } from '@/utils/conversationTimeline'

describe('buildConversationTimeline', () => {
  it('groups OpenAI messages into turns and exposes tool calls/results', () => {
    const timeline = buildConversationTimeline(
      JSON.stringify({
        messages: [
          { role: 'user', content: '查一下天气' },
          {
            role: 'assistant',
            tool_calls: [{ id: 'call_1', type: 'function', function: { name: 'weather', arguments: '{"city":"上海"}' } }]
          },
          { role: 'tool', tool_call_id: 'call_1', content: '{"temp":26}' }
        ]
      }),
      JSON.stringify({ choices: [{ message: { role: 'assistant', content: '上海今天 26°C' } }] })
    )

    expect(timeline.rounds).toHaveLength(1)
    expect(timeline.messageCount).toBe(4)
    expect(timeline.operationCount).toBe(2)
    expect(timeline.rounds[0].messages[1].operations[0]).toMatchObject({
      kind: 'call',
      name: 'weather',
      callId: 'call_1',
      input: { city: '上海' }
    })
    expect(timeline.rounds[0].messages[2].operations[0]).toMatchObject({
      kind: 'result',
      callId: 'call_1',
      output: { temp: 26 }
    })
  })

  it('aggregates streaming text deltas into one assistant message', () => {
    const timeline = buildConversationTimeline(
      JSON.stringify({ messages: [{ role: 'user', content: 'hello' }] }),
      [
        'data: {"choices":[{"delta":{"role":"assistant","content":"hel"}}]}',
        '',
        'data: {"choices":[{"delta":{"content":"lo"}}]}',
        '',
        'data: [DONE]',
        ''
      ].join('\n')
    )

    expect(timeline.rounds[0].messages.at(-1)?.parts[0].text).toBe('hello')
  })

  it('normalizes Anthropic tool blocks', () => {
    const timeline = buildConversationTimeline(
      JSON.stringify({
        messages: [{
          role: 'assistant',
          content: [{ type: 'tool_use', id: 'tool_1', name: 'search', input: { query: 'Sub2API' } }]
        }]
      }),
      JSON.stringify({
        content: [{ type: 'tool_use', id: 'tool_2', name: 'search', input: { query: 'docs' } }]
      })
    )

    expect(timeline.operationCount).toBe(2)
    expect(timeline.rounds.flatMap((round) => round.messages).map((message) => message.operations[0]?.name)).toEqual(['search', 'search'])
  })
})
