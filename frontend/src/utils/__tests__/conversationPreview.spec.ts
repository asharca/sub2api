import { describe, expect, it } from 'vitest'
import { buildConversationPreview } from '@/utils/conversationPreview'

describe('buildConversationPreview', () => {
  it('uses the latest user message and keeps the row preview compact', () => {
    const preview = buildConversationPreview(
      JSON.stringify({
        messages: [
          { role: 'user', content: 'Earlier question that should not be summarised.' },
          { role: 'assistant', content: 'Earlier reply.' },
          { role: 'user', content: '  Prepare\n\na concise   rollout summary.  ' }
        ]
      }),
      JSON.stringify({ choices: [{ message: { role: 'assistant', content: 'Done.' } }] })
    )

    expect(preview).toMatchObject({
      text: 'Prepare a concise rollout summary.',
      messageCount: 4,
      operationCount: 0
    })
  })

  it('uses the final user message from a multi-turn Responses API input', () => {
    const preview = buildConversationPreview(
      JSON.stringify({
        model: 'gpt-5.6-luna',
        input: [
          { role: 'developer', content: 'You are a helpful coding assistant.' },
          { role: 'user', content: [{ type: 'input_text', text: 'Initial request.' }] },
          { type: 'function_call_output', call_id: 'call_123', output: 'The file was read.' },
          { role: 'user', content: [{ type: 'input_text', text: 'Apply the final change and explain it briefly.' }] }
        ]
      }),
      JSON.stringify({ output: [{ type: 'message', role: 'assistant', content: [{ type: 'output_text', text: 'Done.' }] }] })
    )

    expect(preview.text).toBe('Apply the final change and explain it briefly.')
  })

  it('does not surface an unparseable protocol payload as conversation text', () => {
    const preview = buildConversationPreview('{"messages":', '')

    expect(preview.text).toBe('')
    expect(preview.messageCount).toBe(1)
  })
})
