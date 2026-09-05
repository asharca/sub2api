<template>
  <Bubble class="record-message" :class="{ 'is-user': message.role === 'user', 'is-compact': compact }" variant="borderless" max-width="100%" :no-style="true">
    <template #avatar>
      <span class="flex h-7 w-7 items-center justify-center rounded-md" :class="avatarClass">
        <Icon :name="roleIcon" size="sm" />
      </span>
    </template>
    <template #header>
      <div class="flex items-center gap-2 text-xs" :class="compact ? 'mb-1 min-h-6' : 'mb-2 min-h-7'">
        <span class="font-semibold text-gray-900 dark:text-gray-100">{{ title || text(`roles.${message.role}`) }}</span>
        <span v-if="!title" class="text-gray-400 dark:text-dark-400">{{ text(`sources.${message.source}`) }}</span>
        <button type="button" class="ml-auto rounded p-1 text-gray-400 hover:text-primary-600 focus-visible:ring-2 focus-visible:ring-primary-500" :title="t('common.copy')" :aria-label="t('common.copy')" @click="copyToClipboard(copyText)">
          <Icon :name="copied ? 'check' : 'copy'" size="sm" />
        </button>
      </div>
    </template>
    <template #content>
      <div class="text-gray-700 dark:text-dark-100" :class="compact ? 'space-y-2' : 'space-y-3'">
        <Thinking v-if="reasoning" v-model="reasoningOpen" :content="reasoning" status="start" max-width="100%" button-width="100%" :aria-label="text('reasoning')">
          <template #status-icon><Icon name="brain" size="sm" /></template>
          <template #label>{{ text('reasoning') }}</template>
          <template #arrow><Icon name="chevronDown" size="xs" /></template>
          <template #content><div :class="compact ? 'max-h-44 overflow-y-auto pr-2' : ''"><ConversationContent v-if="reasoningOpen" :content="reasoning" :search="search" /></div></template>
        </Thinking>
        <div v-if="bodyParts.length" :class="compact ? 'max-h-44 overflow-y-auto pr-2' : !expanded && isLong && !search.trim() ? 'max-h-80 overflow-hidden' : ''">
          <div v-for="(part, index) in bodyParts" :key="index" class="mb-2 last:mb-0">
            <details v-if="part.url && /^data:image\/(png|jpeg|gif|webp);base64,/i.test(part.url)">
              <summary class="cursor-pointer text-xs">{{ part.label || 'Image' }}</summary>
              <img :src="part.url" :alt="part.label || 'Image'" loading="lazy" class="mt-2 max-h-96 max-w-full object-contain">
            </details>
            <a v-else-if="part.url && /^https?:\/\//i.test(part.url)" :href="part.url" target="_blank" rel="noopener noreferrer" class="break-all text-primary-600 underline">{{ part.url }}</a>
            <ConversationContent v-else :content="part.text" :search="search" :plain="part.kind !== 'text'" />
          </div>
        </div>
        <button v-if="!compact && isLong && !search.trim()" type="button" class="flex items-center gap-1 text-xs font-medium text-primary-600 dark:text-primary-300" :aria-expanded="expanded" @click="expanded = !expanded">
          <Icon name="chevronDown" size="xs" :class="expanded ? 'rotate-180' : ''" />
          {{ expanded ? text('collapseMessage') : text('showFullMessage') }}
        </button>
        <details v-for="operation in message.operations" :key="operation.id" class="operation group border-l-2 border-gray-200 pl-3 dark:border-dark-600" :open="compact || Boolean(search.trim())">
          <summary class="flex cursor-pointer list-none flex-wrap items-center gap-2 py-1.5 text-xs">
            <Icon name="terminal" size="sm" class="text-gray-400" />
            <div class="min-w-0 font-mono font-semibold"><ConversationContent :content="operation.name" :search="search" plain /></div>
            <span :class="operation.kind === 'result' ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-500'">{{ text(`operationKinds.${operation.kind}`) }}</span>
            <span v-if="operation.callId" class="min-w-0 max-w-40 truncate font-mono text-[10px] text-gray-400" :title="operation.callId">{{ operation.callId }}</span>
            <Icon name="chevronDown" size="xs" class="ml-auto shrink-0 text-gray-400 group-open:rotate-180" />
          </summary>
          <div class="space-y-2 py-1.5" :class="compact ? 'max-h-44 overflow-y-auto pr-2' : 'py-2'">
            <div v-if="operation.input !== undefined">
              <div class="text-xs font-medium text-gray-500">{{ text('input') }}</div>
              <ConversationContent :content="formatValue(operation.input)" :search="search" plain />
            </div>
            <div v-if="operation.output !== undefined">
              <div class="text-xs font-medium text-gray-500">{{ text('output') }}</div>
              <ConversationContent :content="formatValue(operation.output)" :search="search" plain />
            </div>
          </div>
        </details>
        <p v-if="!message.parts.length && !message.operations.length" class="text-xs text-gray-400">{{ text('emptyMessage') }}</p>
      </div>
    </template>
  </Bubble>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Bubble from 'vue-element-plus-x/es/Bubble/index.js'
import Thinking from 'vue-element-plus-x/es/Thinking/index.js'
import Icon from '@/components/icons/Icon.vue'
import ConversationContent from './ConversationContent.vue'
import { useClipboard } from '@/composables/useClipboard'
import { formatValue, type ConversationMessage } from '@/utils/conversationTimeline'

const props = withDefaults(defineProps<{ message: ConversationMessage; i18nPrefix: string; title?: string; search?: string; compact?: boolean }>(), { title: '', search: '', compact: false })
const { t } = useI18n()
const { copied, copyToClipboard } = useClipboard()
const expanded = ref(false)
const reasoningOpen = ref(props.compact)
const text = (key: string) => t(`${props.i18nPrefix}.timeline.${key}`)
const reasoning = computed(() => props.message.parts.filter(part => part.label === 'reasoning').map(part => part.text).join('\n\n'))
const bodyParts = computed(() => props.message.parts.filter(part => part.label !== 'reasoning'))
const isLong = computed(() => bodyParts.value.reduce((size, part) => size + part.text.length, 0) > 1800)
const copyText = computed(() => [...props.message.parts.map(part => part.text), ...props.message.operations.map(op => `${op.name}\n${formatValue(op.input)}\n${formatValue(op.output)}`)].join('\n\n'))
const roleIcon = computed(() => ({ user: 'user', assistant: 'brain', tool: 'terminal', system: 'document', unknown: 'chat' } as const)[props.message.role])
const avatarClass = computed(() => ({ user: 'bg-sky-100 text-sky-700 dark:bg-sky-500/15 dark:text-sky-300', assistant: 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-300', tool: 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-300', system: 'bg-gray-100 text-gray-500 dark:bg-dark-700', unknown: 'bg-gray-100 text-gray-500 dark:bg-dark-700' })[props.message.role])
watch([() => props.search, () => props.compact], ([search, compact]) => { if (compact || search.trim()) reasoningOpen.value = true }, { immediate: true })
</script>

<style scoped>
.record-message { --el-font-size-base: 14px; --el-font-line-height-primary: 1.7; --el-text-color-primary: inherit; --elx-text-color-regular: inherit; }
.record-message :deep(.elx-bubble__header), .record-message :deep(.elx-bubble__content) { width: 100%; }
.is-user :deep(.elx-bubble__content) { background: rgb(127 127 127 / 6%) !important; border-radius: 6px; padding: 12px 16px !important; }
.record-message :deep(.elx-thinking__trigger) { padding: 8px 0; color: inherit; background: transparent; border: 0; font-size: 12px; }
.record-message :deep(.elx-thinking__content) { padding: 12px; }
.record-message :deep(.elx-thinking__content-wrapper) { border-radius: 6px; }
.operation summary :deep(pre) { background: none; padding: 0; margin: 0; font-size: 12px; }
.is-compact :deep(.elx-bubble__content) { padding: 8px 10px !important; }
.is-compact :deep(.conversation-content) { font-size: 12px; line-height: 1.55; }
.is-compact :deep(.elx-thinking__trigger) { padding: 4px 0; }
.is-compact :deep(.elx-thinking__content) { padding: 8px; }
</style>
