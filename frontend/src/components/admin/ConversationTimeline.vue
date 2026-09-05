<template>
  <section class="conversation-reader text-gray-700 dark:text-dark-100">
    <div v-if="timeline.rounds.length">
      <header class="flex flex-wrap items-center gap-2 border-b border-gray-200 pb-3 dark:border-dark-700">
        <div class="flex shrink-0 overflow-hidden rounded-md border border-gray-200 dark:border-dark-600" :aria-label="text('jumpToRound')">
          <button
            v-for="order in (['asc', 'desc'] as const)"
            :key="order"
            type="button"
            class="flex h-9 w-9 items-center justify-center transition-colors"
            :class="sortOrder === order ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900' : 'hover:bg-gray-100 dark:hover:bg-dark-700'"
            :aria-label="text(order === 'asc' ? 'oldestFirst' : 'newestFirst')"
            :aria-pressed="sortOrder === order"
            :title="text(order === 'asc' ? 'oldestFirst' : 'newestFirst')"
            @click="sortOrder = order"
          >
            <Icon :name="order === 'asc' ? 'arrowUp' : 'arrowDown'" size="xs" />
          </button>
        </div>
        <label class="relative ml-auto w-full sm:w-72">
          <Icon name="search" size="xs" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
          <input v-model="search" type="search" class="input h-9 w-full py-1.5 pl-8 text-xs" :placeholder="text('searchPlaceholder')" :aria-label="text('searchPlaceholder')">
        </label>
      <span v-if="query" class="text-xs text-primary-600 dark:text-primary-300" role="status">{{ text('searchResultCount', { count: matchCount }) }}</span>
      </header>

      <div class="reader-layout">
        <aside class="round-navigation border-gray-200 dark:border-dark-700" :aria-label="text('jumpToRound')">
          <nav class="round-list space-y-1" :aria-label="text('history')">
          <button v-for="round in matchingRounds" :key="round.id" type="button" class="w-full rounded-md border-l-2 px-3 py-2.5 text-left transition-colors" :class="activeRound?.id === round.id ? 'border-primary-500 bg-primary-50 dark:bg-primary-500/10' : 'border-transparent hover:bg-gray-50 dark:hover:bg-dark-700/50'" :aria-current="activeRound?.id === round.id ? 'step' : undefined" @click="selectRound(round.id, false)">
            <span class="flex items-center justify-between gap-2 text-xs font-medium">
              {{ roundTitle(round) }}
              <span v-if="round.id === latestRound?.id" class="text-[10px] text-primary-600 dark:text-primary-300">{{ text('latest') }}</span>
            </span>
            <span class="mt-1 block line-clamp-2 break-words text-xs leading-5 text-gray-500 dark:text-dark-300">{{ roundPreview(round) }}</span>
            <span class="mt-1 block text-[10px] text-gray-400">{{ text('roundMessageCount', { count: round.messages.length }) }}</span>
          </button>
          </nav>
        </aside>

      <main ref="readingPane" class="reading-pane" :aria-label="activeRound ? roundTitle(activeRound) : text('title')">
        <template v-if="activeRound">
          <div class="mb-5 flex items-center justify-between gap-3 border-b border-gray-100 pb-3 dark:border-dark-700">
            <h4 class="text-sm font-semibold">{{ roundTitle(activeRound) }}<span v-if="activeRound.id === latestRound?.id" class="ml-2 text-xs font-normal text-gray-400">{{ text('latestExchange') }}</span></h4>
            <button v-if="activeRound.id !== latestRound?.id" type="button" class="flex items-center gap-1 text-xs text-primary-600 dark:text-primary-300" @click="selectRound(latestRound!.id)"><Icon name="arrowRight" size="xs" />{{ text('latestExchange') }}</button>
          </div>
          <div class="space-y-6">
            <ConversationMessage v-for="message in pinnedMessages" :key="message.id" :message="message" :title="text(message.role === 'user' ? 'lastUserInput' : 'finalResponse')" :i18n-prefix="i18nPrefix" :search="search" />
          </div>
          <details v-if="historyMessages.length" :key="`${activeRound.id}-${query}`" class="history group/history mt-4 border-t border-gray-200 pt-2 dark:border-dark-700" :open="Boolean(query)">
            <summary class="flex cursor-pointer list-none items-center gap-2 py-1 text-xs font-semibold text-gray-500 dark:text-dark-300">
              <Icon name="chevronDown" size="xs" class="group-open/history:rotate-180" />
              {{ text('history') }} · {{ text('messageCount', { count: historyMessages.length }) }}
            </summary>
            <div class="mt-2 space-y-3">
              <ConversationMessage v-for="message in historyMessages" :key="message.id" compact :message="message" :i18n-prefix="i18nPrefix" :search="search" />
            </div>
          </details>
        </template>
        <p v-else class="py-12 text-center text-sm text-gray-500" role="status">{{ text('noSearchResults') }}</p>
      </main>
      </div>
    </div>
    <div v-else class="py-12 text-center text-sm text-gray-500">
      <p>{{ text('noMessages') }}</p>
      <p class="mt-2 text-xs">{{ text('noMessagesHint') }}</p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import ConversationMessage from './ConversationMessage.vue'
import { buildConversationTimeline, formatValue, type ConversationMessage as Message, type ConversationRound } from '@/utils/conversationTimeline'

const props = withDefaults(defineProps<{
  requestBody: string
  responseBody: string
  i18nPrefix: string
  requestTruncated?: boolean
  responseTruncated?: boolean
}>(), { requestTruncated: false, responseTruncated: false })

const { t } = useI18n()
const text = (key: string, params: Record<string, unknown> = {}) => t(`${props.i18nPrefix}.timeline.${key}`, params)
const timeline = computed(() => buildConversationTimeline(props.requestBody, props.responseBody))
const search = ref('')
const query = computed(() => search.value.trim().toLocaleLowerCase())
const sortOrder = ref<'asc' | 'desc'>('desc')
const selectedRoundID = ref('')
const readingPane = ref<HTMLElement>()
const latestRound = computed(() => timeline.value.rounds.at(-1))
const searchableMessages = computed(() => new Map(timeline.value.rounds.flatMap(round => round.messages.map(message => [message.id, [
  ...message.parts.flatMap(part => [part.label, part.text, part.url]),
  ...message.operations.flatMap(op => [op.name, op.callId, formatValue(op.input), formatValue(op.output)])
].filter(Boolean).join('\n').toLocaleLowerCase()] as const))))
const matches = (message: Message) => !query.value || searchableMessages.value.get(message.id)?.includes(query.value)
const matchingRounds = computed(() => {
  const rounds = timeline.value.rounds.filter(round => round.messages.some(matches))
  return sortOrder.value === 'desc' ? rounds.reverse() : rounds
})
const activeRound = computed(() => matchingRounds.value.find(round => round.id === selectedRoundID.value)
  || (query.value ? matchingRounds.value[0] : latestRound.value))
const matchCount = computed(() => timeline.value.rounds.reduce((count, round) => count + round.messages.filter(matches).length, 0))

function lastUser(round: ConversationRound) {
  return [...round.messages].reverse().find(message => message.role === 'user' && !message.operations.some(op => op.kind === 'result'))
}
function finalAnswer(round: ConversationRound) {
  const user = lastUser(round)
  const messages = round.messages.slice(user ? round.messages.indexOf(user) + 1 : 0)
  return [...messages].reverse().find(message => message.role === 'assistant' && message.parts.some(part => part.label !== 'reasoning' && part.text.trim()))
}
const highlightedMessages = computed(() => activeRound.value ? [lastUser(activeRound.value), finalAnswer(activeRound.value)].filter((message): message is Message => Boolean(message)) : [])
const pinnedMessages = computed(() => {
  const messages = highlightedMessages.value.filter(matches)
  return sortOrder.value === 'desc' ? messages.reverse() : messages
})
const historyMessages = computed(() => {
  const pinned = new Set(highlightedMessages.value.map(message => message.id))
  const messages = activeRound.value?.messages.filter(message => !pinned.has(message.id) && matches(message)) || []
  return sortOrder.value === 'desc' ? messages.reverse() : messages
})
function roundTitle(round: ConversationRound) {
  return round.messages.some(message => message.role === 'user') ? text('round', { index: round.index }) : text('context')
}
function roundPreview(round: ConversationRound) {
  return (lastUser(round)?.parts.filter(part => part.label !== 'reasoning').map(part => part.text).join(' ') || text('context')).slice(0, 160)
}
async function selectRound(id: string, clearSearch = true) {
  if (clearSearch) search.value = ''
  selectedRoundID.value = id
  await nextTick()
  if (readingPane.value) readingPane.value.scrollTop = 0
}
</script>

<style scoped>
.reader-layout { display: grid; grid-template-columns: 232px minmax(0, 1fr); height: min(76vh, 920px); min-height: 340px; }
.round-navigation { display: flex; flex-direction: column; min-height: 0; padding: 16px 12px 0 0; border-right-width: 1px; }
.round-list { min-height: 0; overflow-y: auto; }
.reading-pane { flex: 1; min-width: 0; min-height: 0; overflow-y: auto; padding: 20px 28px 32px; scrollbar-gutter: stable; }
@media (max-width: 767px) {
  .reader-layout { display: flex; flex-direction: column; height: 72vh; min-height: 280px; }
  .round-navigation { flex-shrink: 0; padding: 12px 0 0; border-right: 0; }
  .round-list { display: none; }
  .reading-pane { padding: 12px 0 20px; }
}
</style>
