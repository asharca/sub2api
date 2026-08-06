import { describe, expect, it } from 'vitest'
import { buildConversationPreview } from '@/utils/conversationPreview'

describe('buildConversationPreview', () => {
  it('uses the first user message and keeps the row preview compact', () => {
    const preview = buildConversationPreview(
      JSON.stringify({ messages: [{ role: 'user', content: '  Prepare\n\na concise   rollout summary.  ' }] }),
      JSON.stringify({ choices: [{ message: { role: 'assistant', content: 'Done.' } }] })
    )

    expect(preview).toMatchObject({
      text: 'Prepare a concise rollout summary.',
      messageCount: 2,
      operationCount: 0
    })
  })

  it('does not surface an unparseable protocol payload as conversation text', () => {
    const preview = buildConversationPreview('{"messages":', '')

    expect(preview.text).toBe('')
    expect(preview.messageCount).toBe(1)
  })
})
