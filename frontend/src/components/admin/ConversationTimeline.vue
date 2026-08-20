<template>
  <section class="overflow-hidden rounded-2xl border border-gray-200 bg-gradient-to-b from-gray-50 to-white dark:border-dark-700 dark:from-dark-900 dark:to-dark-800">
    <div class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-dark-700 sm:px-5">
      <div>
        <div class="flex items-center gap-2">
          <span class="flex h-8 w-8 items-center justify-center rounded-xl bg-primary-100 text-primary-600 dark:bg-primary-500/15 dark:text-primary-300">
            <Icon name="chatBubble" size="sm" />
          </span>
          <div>
            <h3 class="text-sm font-semibold text-gray-950 dark:text-white">{{ text('timeline.title') }}</h3>
            <p class="mt-0.5 text-xs text-gray-500 dark:text-dark-300">{{ text('timeline.description') }}</p>
          </div>
        </div>
      </div>

      <div class="flex w-full flex-wrap items-center justify-between gap-2 text-xs sm:w-auto sm:justify-end">
        <label class="relative w-full sm:w-64">
          <Icon name="search" size="xs" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-400" />
          <input
            v-model="timelineSearch"
            type="search"
            class="input h-9 w-full py-1.5 pl-8 pr-3 text-xs"
            :placeholder="text('timeline.searchPlaceholder')"
            :aria-label="text('timeline.searchPlaceholder')"
          >
        </label>
        <select
          v-if="timeline.rounds.length > 1"
          v-model="jumpRoundID"
          class="input h-9 w-full py-1.5 text-xs sm:w-36"
          :aria-label="text('timeline.jumpToRound')"
          @change="jumpToRound"
        >
          <option value="">{{ text('timeline.jumpToRound') }}</option>
          <option v-for="round in timeline.rounds" :key="round.id" :value="round.id">{{ roundTitle(round) }}</option>
        </select>
        <div class="inline-flex overflow-hidden rounded-lg border border-gray-200 bg-white shadow-sm dark:border-dark-600 dark:bg-dark-800">
          <button
            type="button"
            class="px-2.5 py-1.5 font-semibold transition-colors"
            :class="timelineOrder === 'desc' ? 'bg-primary-600 text-white dark:bg-primary-500' : 'text-gray-500 hover:bg-gray-50 dark:text-dark-300 dark:hover:bg-dark-700'"
            :aria-pressed="timelineOrder === 'desc'"
            @click="timelineOrder = 'desc'"
          >
            {{ text('timeline.newestFirst') }}
          </button>
          <button
            type="button"
            class="px-2.5 py-1.5 font-semibold transition-colors"
            :class="timelineOrder === 'asc' ? 'bg-primary-600 text-white dark:bg-primary-500' : 'text-gray-500 hover:bg-gray-50 dark:text-dark-300 dark:hover:bg-dark-700'"
            :aria-pressed="timelineOrder === 'asc'"
            @click="timelineOrder = 'asc'"
          >
            {{ text('timeline.oldestFirst') }}
          </button>
        </div>
        <span v-if="hasTimelineSearch" class="rounded-full bg-primary-50 px-2.5 py-1 font-medium text-primary-700 ring-1 ring-primary-100 dark:bg-primary-500/15 dark:text-primary-300 dark:ring-primary-500/20">
          {{ text('timeline.searchResultCount', { count: matchingMessageCount }) }}
        </span>
        <span class="rounded-full bg-white px-2.5 py-1 font-medium text-gray-600 ring-1 ring-gray-200 dark:bg-dark-800 dark:text-dark-200 dark:ring-dark-600">
          {{ text('timeline.roundCount', { count: timeline.rounds.length }) }}
        </span>
        <span class="rounded-full bg-white px-2.5 py-1 font-medium text-gray-600 ring-1 ring-gray-200 dark:bg-dark-800 dark:text-dark-200 dark:ring-dark-600">
          {{ text('timeline.messageCount', { count: timeline.messageCount }) }}
        </span>
        <span class="rounded-full bg-indigo-50 px-2.5 py-1 font-medium text-indigo-700 ring-1 ring-indigo-100 dark:bg-indigo-500/15 dark:text-indigo-300 dark:ring-indigo-500/20">
          {{ text('timeline.operationCount', { count: timeline.operationCount }) }}
        </span>
        <span v-if="hasTruncatedPayload" class="rounded-full bg-amber-50 px-2.5 py-1 font-medium text-amber-700 ring-1 ring-amber-100 dark:bg-amber-500/15 dark:text-amber-300 dark:ring-amber-500/20">
          {{ text('timeline.partialPayload') }}
        </span>
      </div>
    </div>

    <div v-if="displayRounds.length" class="max-h-[calc(100vh-8rem)] space-y-5 overflow-y-auto p-4 sm:p-5">
      <section v-for="round in displayRounds" :key="round.id" :ref="(element) => setRoundElement(round.id, element)" class="relative">
        <div class="mb-3 flex flex-wrap items-center gap-2">
          <span class="inline-flex items-center gap-1.5 rounded-full bg-gray-900 px-2.5 py-1 text-xs font-semibold text-white dark:bg-white dark:text-dark-900">
            <Icon name="arrowRight" size="xs" />
            {{ roundTitle(round) }}
          </span>
          <span class="text-xs text-gray-400 dark:text-dark-400">
            {{ text('timeline.roundMessageCount', { count: displayMessages(round).length }) }}
          </span>
        </div>

        <ol class="relative space-y-3 pl-4 before:absolute before:bottom-3 before:left-[1.15rem] before:top-3 before:w-px before:bg-gray-200 dark:before:bg-dark-600 sm:pl-8">
          <li v-for="(message, messageIndex) in displayMessages(round)" :key="message.id" class="relative">
            <span
              class="absolute -left-4 top-4 z-10 flex h-5 w-5 -translate-x-1/2 items-center justify-center rounded-full border-2 border-white shadow-sm dark:border-dark-800 sm:-left-8"
              :class="roleMarkerClass(message.role)"
            >
              <Icon :name="roleIcon(message.role)" size="xs" />
            </span>

            <article class="overflow-hidden rounded-xl border bg-white shadow-sm dark:bg-dark-800" :class="messageBorderClass(message.role)">
              <header class="flex flex-wrap items-center justify-between gap-2 border-b border-gray-100 px-3 py-2.5 dark:border-dark-700 sm:px-4">
                <div class="flex min-w-0 items-center gap-2">
                  <span class="text-xs font-semibold" :class="roleTextClass(message.role)">
                    {{ text(`timeline.roles.${message.role}`) }}
                  </span>
                  <span class="rounded bg-gray-100 px-1.5 py-0.5 text-[10px] font-medium text-gray-500 dark:bg-dark-700 dark:text-dark-300">
                    {{ text(`timeline.sources.${message.source}`) }}
                  </span>
                </div>
                <span class="text-[10px] tabular-nums text-gray-400 dark:text-dark-400">
                  {{ text('timeline.messageIndex', { index: messageIndex + 1 }) }}
                </span>
              </header>

              <div class="p-3 sm:p-4">
                <div class="relative">
                    <div class="space-y-3" :class="isMessageCollapsed(message) ? 'max-h-96 overflow-hidden' : ''">
                    <div v-if="message.parts.length" class="space-y-2">
                      <div v-for="(part, partIndex) in message.parts" :key="`${message.id}-part-${partIndex}`" class="rounded-lg bg-gray-50 px-3 py-2.5 dark:bg-dark-900/70">
                        <div v-if="part.label" class="mb-1 text-[10px] font-semibold uppercase tracking-wide text-gray-400 dark:text-dark-500">{{ part.label }}</div>
                        <p class="whitespace-pre-wrap break-words text-sm leading-6 text-gray-700 dark:text-dark-100" :class="part.kind === 'media' ? 'font-mono text-xs text-gray-500 dark:text-dark-300' : ''">
                          <template v-for="(segment, segmentIndex) in highlightedSegments(part.text)" :key="`${message.id}-part-${partIndex}-segment-${segmentIndex}`">
                            <mark v-if="segment.match" class="rounded bg-amber-200 px-0.5 text-inherit dark:bg-amber-400/35">{{ segment.text }}</mark>
                            <template v-else>{{ segment.text }}</template>
                          </template>
                        </p>
                      </div>
                    </div>

                    <div v-if="message.operations.length" class="space-y-2">
                      <div v-for="operation in message.operations" :key="operation.id" class="rounded-xl border border-indigo-100 bg-indigo-50/60 p-3 dark:border-indigo-500/20 dark:bg-indigo-500/10">
                        <div class="flex flex-wrap items-center gap-2">
                          <span class="flex h-6 w-6 items-center justify-center rounded-lg bg-indigo-100 text-indigo-700 dark:bg-indigo-500/20 dark:text-indigo-300">
                            <Icon name="terminal" size="xs" />
                          </span>
                          <span class="text-xs font-semibold text-indigo-900 dark:text-indigo-100">{{ operation.name }}</span>
                          <span class="rounded-full px-2 py-0.5 text-[10px] font-semibold" :class="operation.kind === 'call' ? 'bg-indigo-100 text-indigo-700 dark:bg-indigo-500/20 dark:text-indigo-300' : 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/20 dark:text-emerald-300'">
                            {{ text(`timeline.operationKinds.${operation.kind}`) }}
                          </span>
                          <span v-if="operation.callId" class="max-w-[14rem] truncate font-mono text-[10px] text-indigo-500 dark:text-indigo-300" :title="operation.callId">
                            {{ operation.callId }}
                          </span>
                        </div>

                        <div v-if="operation.input !== undefined || operation.output !== undefined" class="mt-2 grid gap-2 lg:grid-cols-2">
                          <div v-if="operation.input !== undefined" class="overflow-hidden rounded-lg border border-indigo-100 bg-white/80 dark:border-indigo-500/20 dark:bg-dark-900/60">
                            <div class="border-b border-indigo-100 px-2.5 py-1.5 text-[10px] font-semibold uppercase tracking-wide text-indigo-500 dark:border-indigo-500/20 dark:text-indigo-300">{{ text('timeline.input') }}</div>
                            <pre class="max-h-48 overflow-auto whitespace-pre-wrap break-words p-2.5 font-mono text-[11px] leading-5 text-gray-700 dark:text-dark-100"><template v-for="(segment, segmentIndex) in highlightedSegments(formatValue(operation.input))" :key="`${operation.id}-input-${segmentIndex}`"><mark v-if="segment.match" class="rounded bg-amber-200 px-0.5 text-inherit dark:bg-amber-400/35">{{ segment.text }}</mark><template v-else>{{ segment.text }}</template></template></pre>
                          </div>
                          <div v-if="operation.output !== undefined" class="overflow-hidden rounded-lg border border-emerald-100 bg-white/80 dark:border-emerald-500/20 dark:bg-dark-900/60">
                            <div class="border-b border-emerald-100 px-2.5 py-1.5 text-[10px] font-semibold uppercase tracking-wide text-emerald-600 dark:border-emerald-500/20 dark:text-emerald-300">{{ text('timeline.output') }}</div>
                            <pre class="max-h-48 overflow-auto whitespace-pre-wrap break-words p-2.5 font-mono text-[11px] leading-5 text-gray-700 dark:text-dark-100"><template v-for="(segment, segmentIndex) in highlightedSegments(formatValue(operation.output))" :key="`${operation.id}-output-${segmentIndex}`"><mark v-if="segment.match" class="rounded bg-amber-200 px-0.5 text-inherit dark:bg-amber-400/35">{{ segment.text }}</mark><template v-else>{{ segment.text }}</template></template></pre>
                          </div>
                        </div>
                      </div>
                    </div>

                    <div v-if="!message.parts.length && !message.operations.length" class="text-xs italic text-gray-400 dark:text-dark-400">{{ text('timeline.emptyMessage') }}</div>
                  </div>
                  <div v-if="isMessageCollapsed(message)" class="pointer-events-none absolute inset-x-0 bottom-0 h-20 bg-gradient-to-t from-white via-white/95 to-transparent dark:from-dark-800 dark:via-dark-800/95"></div>
                </div>
                <button
                  v-if="shouldCollapseMessage(message)"
                  type="button"
                  class="mt-3 inline-flex items-center gap-1.5 text-xs font-semibold text-primary-600 hover:text-primary-700 dark:text-primary-300 dark:hover:text-primary-200"
                  :aria-expanded="!isMessageCollapsed(message)"
                  @click="toggleMessage(message.id)"
                >
                  <Icon name="chevronDown" size="xs" :class="isMessageCollapsed(message) ? '' : 'rotate-180'" />
                  {{ isMessageCollapsed(message) ? text('timeline.showFullMessage') : text('timeline.collapseMessage') }}
                </button>
              </div>
            </article>
          </li>
        </ol>
      </section>
    </div>

    <div v-else class="flex flex-col items-center px-5 py-12 text-center">
      <span class="flex h-12 w-12 items-center justify-center rounded-2xl bg-gray-100 text-gray-400 dark:bg-dark-700 dark:text-dark-400">
        <Icon name="document" size="lg" />
      </span>
      <p class="mt-3 text-sm font-medium text-gray-700 dark:text-dark-200">{{ hasTimelineSearch ? text('timeline.noSearchResults') : text('timeline.noMessages') }}</p>
      <p class="mt-1 max-w-lg text-xs leading-5 text-gray-500 dark:text-dark-400">{{ hasTimelineSearch ? text('timeline.noSearchResultsHint') : text('timeline.noMessagesHint') }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import { buildConversationTimeline, formatValue, type ConversationMessage, type ConversationRole, type ConversationRound } from '@/utils/conversationTimeline'

const props = withDefaults(defineProps<{
  requestBody: string
  responseBody: string
  i18nPrefix: string
  requestTruncated?: boolean
  responseTruncated?: boolean
}>(), {
  requestTruncated: false,
  responseTruncated: false
})

const { t } = useI18n()
const timeline = computed(() => buildConversationTimeline(props.requestBody, props.responseBody))
const timelineOrder = ref<'asc' | 'desc'>('asc')
const timelineSearch = ref('')
const jumpRoundID = ref('')
const expandedMessages = ref(new Set<string>())
const roundElements = new Map<string, HTMLElement>()
const hasTruncatedPayload = computed(() => props.requestTruncated || props.responseTruncated)
const normalizedTimelineSearch = computed(() => timelineSearch.value.trim().toLocaleLowerCase())
const hasTimelineSearch = computed(() => normalizedTimelineSearch.value.length > 0)
const matchingRounds = computed(() => hasTimelineSearch.value
  ? timeline.value.rounds.filter((round) => round.messages.some(matchesMessage))
  : timeline.value.rounds
)
const displayRounds = computed(() => timelineOrder.value === 'desc' ? [...matchingRounds.value].reverse() : matchingRounds.value)
const matchingMessageCount = computed(() => hasTimelineSearch.value
  ? matchingRounds.value.reduce((total, round) => total + round.messages.filter(matchesMessage).length, 0)
  : timeline.value.messageCount
)

function text(key: string, params?: Record<string, unknown>) {
  const path = `${props.i18nPrefix}.${key}`
  return params ? t(path, params) : t(path)
}

function roundTitle(round: ConversationRound) {
  const hasUserMessage = round.messages.some((message) => message.role === 'user')
  return hasUserMessage ? text('timeline.round', { index: round.index }) : text('timeline.context')
}

function displayMessages(round: ConversationRound) {
  const messages = hasTimelineSearch.value ? round.messages.filter(matchesMessage) : round.messages
  return timelineOrder.value === 'desc' ? [...messages].reverse() : messages
}

function setRoundElement(roundID: string, element: unknown) {
  if (element instanceof HTMLElement) {
    roundElements.set(roundID, element)
  } else {
    roundElements.delete(roundID)
  }
}

async function jumpToRound() {
  const roundID = jumpRoundID.value
  if (!roundID) return
  if (hasTimelineSearch.value) timelineSearch.value = ''
  await nextTick()
  roundElements.get(roundID)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  jumpRoundID.value = ''
}

function matchesMessage(message: ConversationMessage) {
  if (!hasTimelineSearch.value) return true
  const searchable = [
    ...message.parts.flatMap((part) => [part.label, part.text]),
    ...message.operations.flatMap((operation) => [
      operation.name,
      operation.callId,
      formatValue(operation.input),
      formatValue(operation.output)
    ])
  ].filter((value): value is string => Boolean(value)).join('\n').toLocaleLowerCase()
  return searchable.includes(normalizedTimelineSearch.value)
}

function highlightedSegments(value: string) {
  const query = timelineSearch.value.trim()
  if (!query) return [{ text: value, match: false }]
  const lowerValue = value.toLocaleLowerCase()
  const lowerQuery = query.toLocaleLowerCase()
  const segments: Array<{ text: string; match: boolean }> = []
  let start = 0
  let index = lowerValue.indexOf(lowerQuery)
  while (index >= 0) {
    if (index > start) segments.push({ text: value.slice(start, index), match: false })
    segments.push({ text: value.slice(index, index + query.length), match: true })
    start = index + query.length
    index = lowerValue.indexOf(lowerQuery, start)
  }
  if (start < value.length) segments.push({ text: value.slice(start), match: false })
  return segments
}

function shouldCollapseMessage(message: ConversationMessage) {
  const contentLength = message.parts.reduce((total, part) => total + part.text.length, 0)
  const operationLength = message.operations.reduce(
    (total, operation) => total + formatValue(operation.input).length + formatValue(operation.output).length,
    0
  )
  return contentLength + operationLength > 1200 || message.operations.length > 2
}

function isMessageCollapsed(message: ConversationMessage) {
  return !hasTimelineSearch.value && shouldCollapseMessage(message) && !expandedMessages.value.has(message.id)
}

function toggleMessage(messageId: string) {
  const next = new Set(expandedMessages.value)
  if (next.has(messageId)) {
    next.delete(messageId)
  } else {
    next.add(messageId)
  }
  expandedMessages.value = next
}

function roleIcon(role: ConversationRole): 'chat' | 'user' | 'brain' | 'terminal' | 'document' {
  switch (role) {
    case 'user': return 'user'
    case 'assistant': return 'brain'
    case 'tool': return 'terminal'
    case 'system': return 'document'
    default: return 'chat'
  }
}

function roleMarkerClass(role: ConversationRole) {
  switch (role) {
    case 'user': return 'bg-sky-500 text-white'
    case 'assistant': return 'bg-violet-500 text-white'
    case 'tool': return 'bg-amber-500 text-white'
    case 'system': return 'bg-gray-500 text-white'
    default: return 'bg-gray-400 text-white'
  }
}

function roleTextClass(role: ConversationRole) {
  switch (role) {
    case 'user': return 'text-sky-700 dark:text-sky-300'
    case 'assistant': return 'text-violet-700 dark:text-violet-300'
    case 'tool': return 'text-amber-700 dark:text-amber-300'
    case 'system': return 'text-gray-600 dark:text-dark-200'
    default: return 'text-gray-600 dark:text-dark-200'
  }
}

function messageBorderClass(role: ConversationRole) {
  switch (role) {
    case 'user': return 'border-sky-100 dark:border-sky-500/20'
    case 'assistant': return 'border-violet-100 dark:border-violet-500/20'
    case 'tool': return 'border-amber-100 dark:border-amber-500/20'
    default: return 'border-gray-200 dark:border-dark-700'
  }
}
</script>
