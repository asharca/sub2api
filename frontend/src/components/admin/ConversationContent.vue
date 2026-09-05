<template>
  <div class="conversation-content" v-html="html"></div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { Marked } from 'marked'
import DOMPurify from 'dompurify'

const props = withDefaults(defineProps<{ content: string; search?: string; plain?: boolean }>(), {
  search: '',
  plain: false
})

const markdown = new Marked({ breaks: true, gfm: true })
// Logs are untrusted. Images stay links so opening a record cannot contact tracking URLs.
markdown.use({ renderer: {
  html({ text }) {
    const span = document.createElement('span')
    span.textContent = text
    return span.innerHTML
  },
  image({ href, text }) {
    const link = document.createElement('a')
    link.setAttribute('href', href)
    link.textContent = text || href
    return link.outerHTML
  }
} })

const html = computed(() => {
  const container = document.createElement('div')
  if (props.plain) {
    const pre = document.createElement('pre')
    pre.textContent = props.content
    container.append(pre)
  } else {
    container.innerHTML = DOMPurify.sanitize(markdown.parse(props.content, { async: false }), {
      ALLOWED_TAGS: ['p', 'br', 'hr', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'strong', 'em', 'del', 'code', 'pre', 'blockquote', 'ul', 'ol', 'li', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'a'],
      ALLOWED_ATTR: ['href', 'title', 'start', 'colspan', 'rowspan']
    })
  }
  for (const link of container.querySelectorAll('a')) {
    const href = link.getAttribute('href') || ''
    if (!/^https?:\/\//i.test(href)) link.removeAttribute('href')
    link.setAttribute('target', '_blank')
    link.setAttribute('rel', 'noopener noreferrer')
  }
  const query = props.search.trim()
  if (query) {
    const pattern = new RegExp(query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'), 'giu')
    const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT)
    const nodes: Text[] = []
    while (walker.nextNode()) nodes.push(walker.currentNode as Text)
    for (const node of nodes) {
      const fragment = document.createDocumentFragment()
      let offset = 0
      for (const match of node.data.matchAll(pattern)) {
        fragment.append(node.data.slice(offset, match.index))
        const mark = document.createElement('mark')
        mark.textContent = match[0]
        fragment.append(mark)
        offset = match.index! + match[0].length
      }
      fragment.append(node.data.slice(offset))
      node.replaceWith(fragment)
    }
  }
  return container.innerHTML
})
</script>

<style scoped>
.conversation-content { min-width: 0; font-size: 14px; line-height: 1.8; overflow-wrap: anywhere; }
.conversation-content :deep(> :first-child) { margin-top: 0; }
.conversation-content :deep(> :last-child) { margin-bottom: 0; }
.conversation-content :deep(p) { margin: 0.65em 0; }
.conversation-content :deep(h1), .conversation-content :deep(h2), .conversation-content :deep(h3),
.conversation-content :deep(h4), .conversation-content :deep(h5), .conversation-content :deep(h6) { margin: 1em 0 0.5em; font-size: 16px; font-weight: 600; line-height: 1.5; }
.conversation-content :deep(ul) { list-style: disc; padding-left: 1.5em; }
.conversation-content :deep(ol) { list-style: decimal; padding-left: 1.5em; }
.conversation-content :deep(li) { margin: 0.25em 0; }
.conversation-content :deep(pre) { overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; padding: 12px; margin: 0.75em 0; border-radius: 6px; background: rgb(127 127 127 / 8%); font-size: 12px; line-height: 1.7; tab-size: 2; }
.conversation-content :deep(code) { font-family: ui-monospace, monospace; font-size: 0.9em; background: rgb(127 127 127 / 10%); padding: 0.1em 0.3em; border-radius: 3px; }
.conversation-content :deep(pre code) { padding: 0; background: none; font-size: inherit; }
.conversation-content :deep(blockquote) { border-left: 3px solid #9ca3af; margin: 0.75em 0; padding-left: 1em; opacity: 0.85; }
.conversation-content :deep(table) { display: block; max-width: 100%; overflow-x: auto; border-collapse: collapse; margin: 0.75em 0; font-size: 13px; }
.conversation-content :deep(th), .conversation-content :deep(td) { border: 1px solid rgb(127 127 127 / 30%); padding: 6px 12px; text-align: left; }
.conversation-content :deep(th) { font-weight: 600; background: rgb(127 127 127 / 8%); }
.conversation-content :deep(a[href]) { color: #0284c7; text-decoration: underline; text-underline-offset: 2px; }
.conversation-content :deep(hr) { margin: 1em 0; border-color: rgb(127 127 127 / 25%); }
.conversation-content :deep(mark) { color: inherit; background: rgb(250 204 21 / 45%); border-radius: 2px; }
</style>
