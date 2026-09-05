import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ConversationContent from '../ConversationContent.vue'
import ConversationTimeline from '../ConversationTimeline.vue'

vi.mock('@/composables/useClipboard', () => ({ useClipboard: () => ({ copied: false, copyToClipboard: vi.fn() }) }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string, params?: { index?: number }) => `${key} ${params?.index || ''}`.trim() }) }))

describe('conversation reader', () => {
  it('renders markdown and highlights literal matches without executing log content', () => {
    const wrapper = mount(ConversationContent, { props: {
      content: '## Result\n\n**file.ts**\n\n```ts\nconst file = "file.ts"\n```\n\n| File | Status |\n| --- | --- |\n| file.ts | OK |\n\n<script>alert(1)</script><img src="https://example.com/tracker" onerror="alert(1)">\n\n[unsafe](javascript:alert(1))\n\n![attachment](https://example.com/image.png)',
      search: 'file.ts'
    } })
    expect(wrapper.find('h2').text()).toBe('Result')
    expect(wrapper.find('table').exists()).toBe(true)
    expect(wrapper.find('pre code').text()).toContain('const file')
    expect(wrapper.findAll('mark')).toHaveLength(3)
    expect(wrapper.find('script, img, [onerror], a[href^="javascript:"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('<script>alert(1)</script>')
    expect(wrapper.find('a[href="https://example.com/image.png"]').attributes('rel')).toBe('noopener noreferrer')
    wrapper.unmount()
  })

  it('starts with the latest exchange and searches across historical rounds, tools and reasoning', async () => {
    const wrapper = mount(ConversationTimeline, {
      props: {
        i18nPrefix: 'conversationLogs',
        requestBody: JSON.stringify({ input: [
          { role: 'user', content: 'First question' },
          { role: 'assistant', content: 'First answer' },
          { role: 'user', content: 'Last question' },
          { type: 'reasoning', summary: [{ type: 'summary_text', text: 'Needle thought' }] },
          { type: 'function_call', name: 'read_file', call_id: 'call-a', arguments: '{"path":"needle.ts"}' },
          { type: 'function_call_output', call_id: 'call-a', output: 'Needle output' }
        ] }),
        responseBody: JSON.stringify({ output: [{ type: 'message', role: 'assistant', content: [{ type: 'output_text', text: '**Final answer**' }] }] })
      }
    })
    expect(wrapper.find('section.conversation-reader').exists()).toBe(true)
    expect(wrapper.find('.reading-pane').text()).toContain('Last question')
    expect(wrapper.find('.reading-pane strong').text()).toBe('Final answer')
    expect(wrapper.find('.reading-pane').text().indexOf('timeline.finalResponse')).toBeLessThan(wrapper.find('.reading-pane').text().indexOf('timeline.lastUserInput'))
    expect(wrapper.find('.reading-pane').text()).not.toContain('First question')
    expect(wrapper.find('.reading-pane > details.history').attributes('open')).toBeUndefined()
    await wrapper.find('input[type="search"]').setValue('Needle')
    expect(wrapper.find('.reading-pane > details.history').attributes('open')).toBeDefined()
    expect(wrapper.find('.history .is-compact').exists()).toBe(true)
    expect(wrapper.findAll('mark').length).toBeGreaterThanOrEqual(3)
    expect(wrapper.find('.operation').attributes('open')).toBeDefined()
    await wrapper.find('input[type="search"]').setValue('First answer')
    expect(wrapper.findAll('.round-list button')).toHaveLength(1)
    expect(wrapper.find('.reading-pane').text()).toContain('First answer')
    await wrapper.find('input[type="search"]').setValue('not-present')
    expect(wrapper.find('.reading-pane').text()).toContain('timeline.noSearchResults')
    await wrapper.find('input[type="search"]').setValue('')
    await wrapper.findAll('.round-list button').find(button => button.text().includes('timeline.round 1'))!.trigger('click')
    expect(wrapper.find('.reading-pane').text()).toContain('First question')
    await wrapper.findAll('button').find(button => button.attributes('aria-label').endsWith('oldestFirst'))!.trigger('click')
    expect(wrapper.find('.round-list button').text()).toContain('timeline.round 1')
    const reverse = wrapper.findAll('button').find(button => button.attributes('aria-label').endsWith('newestFirst'))!
    await reverse.trigger('click')
    expect(wrapper.find('.round-list button').text()).toContain('timeline.round 2')
    await wrapper.setProps({
      requestBody: JSON.stringify({ conversation_turns: [{ turn: 1, request: { input: [
        { role: 'user', content: 'Previous question' },
        { role: 'assistant', content: 'Previous answer' },
        { role: 'user', content: 'New pending question' }
      ] } }] }),
      responseBody: ''
    })
    expect(wrapper.find('.reading-pane').text()).toContain('New pending question')
    expect(wrapper.find('.reading-pane').text()).not.toContain('timeline.finalResponse')
    wrapper.unmount()
  })
})
