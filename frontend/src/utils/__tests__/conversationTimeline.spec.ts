import { describe, expect, it } from 'vitest'
import { buildConversationTimeline } from '@/utils/conversationTimeline'

describe('buildConversationTimeline', () => {
  it('preserves Responses reasoning, tool arguments and final text exactly once', () => {
    const output = [
      { id: 'r1', type: 'reasoning', summary: [{ type: 'summary_text', text: 'Check the file.' }] },
      { id: 'm1', type: 'message', role: 'assistant', content: [{ type: 'output_text', text: 'Checking.' }] },
      { id: 'f1', type: 'function_call', call_id: 'call_1', name: 'read_file', arguments: '{"path":"a.ts"}' }
    ]
    const events = [
      { type: 'response.output_item.added', item: { id: 'r1', type: 'reasoning', summary: [] } },
      { type: 'response.reasoning_summary_text.delta', item_id: 'r1', delta: 'Check the file.' },
      { type: 'response.reasoning_summary_text.done', item_id: 'r1', text: 'Check the file.' },
      { type: 'response.output_text.delta', item_id: 'm1', delta: 'Checking.' },
      { type: 'response.output_text.done', item_id: 'm1', text: 'Checking.' },
      { type: 'response.output_item.added', item: { id: 'f1', type: 'function_call', call_id: 'call_1', name: 'read_file', arguments: '' } },
      { type: 'response.function_call_arguments.delta', item_id: 'f1', delta: '{"path":' },
      { type: 'response.function_call_arguments.delta', item_id: 'f1', delta: '"a.ts"}' },
      ...output.map((item) => ({ type: 'response.output_item.done', item })),
      { type: 'response.completed', response: { output: output.map((item) => ({ ...item, id: `final-${item.id}` })) } }
    ]
    for (const response of [JSON.stringify({ output }), events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join(''), JSON.stringify(events.slice(0, 8))]) {
      const timeline = buildConversationTimeline('', response)
      expect(timeline.messageCount).toBe(3)
      expect(timeline.operationCount).toBe(1)
      expect(timeline.rounds[0].messages.flatMap((message) => message.parts.map((part) => part.text))).toEqual(['Check the file.', 'Checking.'])
      expect(timeline.rounds[0].messages[2].operations[0]).toMatchObject({ name: 'read_file', input: { path: 'a.ts' } })
    }
  })

  it('assembles Chat Completions tool fragments by index even without repeated ids', () => {
    const chunks = [
      { reasoning_content: 'Check first.' },
      { tool_calls: [{ index: 0, id: 'call_a', function: { name: 'read', arguments: '{"p":' } }, { index: 1, id: 'call_b', function: { name: 'find', arguments: '{"q":' } }] },
      { tool_calls: [{ index: 0, function: { arguments: '"a"}' } }, { index: 1, function: { arguments: '"b"}' } }] },
      { content: 'Done.' }
    ]
    const timeline = buildConversationTimeline('', chunks.map((delta) => `data: ${JSON.stringify({ choices: [{ index: 0, delta }] })}\n\n`).join(''))
    expect(timeline.operationCount).toBe(2)
    expect(timeline.rounds[0].messages[0].operations).toMatchObject([
      { name: 'read', callId: 'call_a', input: { p: 'a' } },
      { name: 'find', callId: 'call_b', input: { q: 'b' } }
    ])
    expect(timeline.rounds[0].messages[0].parts.map((part) => part.text)).toEqual(['Check first.', 'Done.'])
  })

  it('retains Anthropic tool input deltas after an empty initial snapshot', () => {
    const events = [
      { type: 'content_block_start', index: 0, content_block: { type: 'tool_use', id: 'call_a', name: 'read', input: {} } },
      { type: 'content_block_delta', index: 0, delta: { type: 'input_json_delta', partial_json: '{"path":' } },
      { type: 'content_block_delta', index: 0, delta: { type: 'input_json_delta', partial_json: '"a.ts"}' } }
    ]
    const timeline = buildConversationTimeline('', events.map((event) => `data: ${JSON.stringify(event)}\n\n`).join(''))
    expect(timeline.operationCount).toBe(1)
    expect(timeline.rounds[0].messages[0].operations[0]).toMatchObject({ name: 'read', input: { path: 'a.ts' } })
  })

  it('keeps long Responses histories including reasoning, custom tools and refusals', () => {
    const history = Array.from({ length: 210 }, (_, index) => [
      { type: 'reasoning', summary: [{ type: 'summary_text', text: `Step ${index}` }] },
      { type: 'custom_tool_call', name: 'patch', call_id: `call_${index}`, input: 'patch text' },
      { type: 'custom_tool_call_output', call_id: `call_${index}`, output: 'patched' }
    ]).flat()
    const timeline = buildConversationTimeline(JSON.stringify({ input: [{ role: 'user', content: 'work' }, ...history] }), JSON.stringify({ output: [{ type: 'message', role: 'assistant', content: [{ type: 'refusal', refusal: 'Cannot continue.' }] }] }))
    expect(timeline.messageCount).toBe(632)
    expect(timeline.operationCount).toBe(420)
    expect(timeline.rounds[0].messages.at(-1)?.parts[0].text).toBe('Cannot continue.')
  })

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

  it('keeps an aggregated WebSocket session in turn order', () => {
    const timeline = buildConversationTimeline(
      JSON.stringify({ conversation_turns: [
        { turn: 1, request: { type: 'response.create', input: 'first' } },
        { turn: 2, request: { type: 'response.create', input: 'second' } }
      ] }),
      JSON.stringify({ conversation_turns: [
        { turn: 1, response: [{ type: 'response.output_text.delta', delta: 'one' }] },
        { turn: 2, response: [{ type: 'response.output_text.delta', delta: 'two' }] }
      ] })
    )

    expect(timeline.rounds).toHaveLength(2)
    expect(timeline.rounds.map((round) => round.messages.map((message) => message.parts[0]?.text))).toEqual([
      ['first', 'one'],
      ['second', 'two']
    ])
    expect(timeline.rounds[0].messages.at(-1)).toMatchObject({ role: 'assistant', source: 'response' })
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
