<template>
  <div class="space-y-3" data-testid="composite-route-rules-editor">
    <div class="flex items-start justify-between gap-3">
      <div class="min-w-0">
        <label class="input-label mb-0">
          {{ t("admin.groups.compositeRoutes.rules.title") }}
          <span class="ml-1 text-xs font-normal text-gray-500 dark:text-gray-400">
            {{ modelValue.length }}/{{ MAX_COMPOSITE_ROUTE_RULES }}
          </span>
        </label>
        <p class="input-hint mb-0">
          {{ t("admin.groups.compositeRoutes.rules.hint") }}
        </p>
      </div>
      <button
        type="button"
        class="inline-flex shrink-0 items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium text-primary-600 transition-colors hover:bg-primary-50 hover:text-primary-700 focus:outline-none focus:ring-2 focus:ring-primary-500/30 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-400 dark:hover:bg-primary-900/20 dark:hover:text-primary-300"
        data-testid="composite-rule-add"
        :disabled="disabled || modelValue.length >= MAX_COMPOSITE_ROUTE_RULES"
        @click="addRule"
      >
        <Icon name="plus" size="sm" />
        {{ t("admin.groups.compositeRoutes.rules.add") }}
      </button>
    </div>

    <p
      v-if="disabled"
      class="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:border-amber-800/60 dark:bg-amber-900/20 dark:text-amber-300"
      role="note"
      data-testid="composite-rule-endpoint-unsupported"
    >
      {{ t("admin.groups.compositeRoutes.rules.endpointUnsupported") }}
    </p>

    <p
      v-if="modelValue.length > MAX_COMPOSITE_ROUTE_RULES"
      class="text-xs text-red-600 dark:text-red-400"
      role="alert"
    >
      {{ t("admin.groups.compositeRoutes.rules.errors.tooMany", { max: MAX_COMPOSITE_ROUTE_RULES }) }}
    </p>

    <div
      v-if="modelValue.length === 0"
      class="rounded-lg border border-dashed border-gray-200 px-3 py-4 text-center text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400"
    >
      {{ t("admin.groups.compositeRoutes.rules.empty") }}
    </div>

    <div
      v-for="(rule, index) in modelValue"
      :key="index"
      :class="[
        'space-y-3 rounded-lg border border-gray-200 bg-gray-50/40 p-3 dark:border-dark-600 dark:bg-dark-800/40',
        !rule.enabled && 'opacity-60',
      ]"
      :data-testid="`composite-rule-${index}`"
    >
      <div class="flex items-center gap-2">
        <span class="badge badge-gray shrink-0">
          {{ t("admin.groups.compositeRoutes.rules.ruleLabel", { index: index + 1 }) }}
        </span>
        <input
          :id="`${idPrefix}-${index}-name`"
          :value="rule.name ?? ''"
          type="text"
          class="input min-w-0 flex-1 py-1.5"
          :maxlength="MAX_COMPOSITE_RULE_NAME_LENGTH"
          :placeholder="t('admin.groups.compositeRoutes.rules.namePlaceholder')"
          :aria-label="t('admin.groups.compositeRoutes.rules.name')"
          :disabled="disabled"
          :data-testid="`composite-rule-name-${index}`"
          @input="updateRule(index, { name: inputValue($event) })"
        />
        <Toggle
          :model-value="rule.enabled"
          :disabled="disabled"
          :title="t('admin.groups.compositeRoutes.rules.enabled')"
          :aria-label="t('admin.groups.compositeRoutes.rules.enabled')"
          @update:model-value="updateRule(index, { enabled: $event })"
        />
        <div class="flex shrink-0 items-center">
          <button
            type="button"
            :class="iconButtonClass"
            :title="t('admin.groups.compositeRoutes.rules.moveUp')"
            :aria-label="t('admin.groups.compositeRoutes.rules.moveUp')"
            :disabled="disabled || index === 0"
            :data-testid="`composite-rule-move-up-${index}`"
            @click="moveRule(index, -1)"
          >
            <Icon name="arrowUp" size="sm" />
          </button>
          <button
            type="button"
            :class="iconButtonClass"
            :title="t('admin.groups.compositeRoutes.rules.moveDown')"
            :aria-label="t('admin.groups.compositeRoutes.rules.moveDown')"
            :disabled="disabled || index === modelValue.length - 1"
            :data-testid="`composite-rule-move-down-${index}`"
            @click="moveRule(index, 1)"
          >
            <Icon name="arrowDown" size="sm" />
          </button>
          <button
            type="button"
            class="rounded p-1.5 text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
            :title="t('admin.groups.compositeRoutes.rules.remove')"
            :aria-label="t('admin.groups.compositeRoutes.rules.remove')"
            :data-testid="`composite-rule-remove-${index}`"
            @click="removeRule(index)"
          >
            <Icon name="trash" size="sm" />
          </button>
        </div>
      </div>

      <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <div>
          <label :for="`${idPrefix}-${index}-platform`" class="input-label">
            {{ t("admin.groups.compositeRoutes.targetPlatform") }}
          </label>
          <Select
            :id="`${idPrefix}-${index}-platform`"
            :model-value="rule.target_platform"
            :options="platformOptions"
            :disabled="disabled"
            @update:model-value="updateRule(index, { target_platform: $event as RulePlatform })"
          />
        </div>
        <div>
          <label :for="`${idPrefix}-${index}-upstream`" class="input-label">
            {{ t("admin.groups.compositeRoutes.upstreamModel") }}
            <span class="text-red-500">*</span>
          </label>
          <input
            :id="`${idPrefix}-${index}-upstream`"
            :value="rule.upstream_model"
            type="text"
            :class="['input', showValidation && hasError(index, 'upstreamModelRequired') && 'input-error']"
            :maxlength="MAX_COMPOSITE_RULE_UPSTREAM_MODEL_LENGTH"
            placeholder="gemini-2.5-pro"
            :disabled="disabled"
            :data-testid="`composite-rule-upstream-${index}`"
            @input="updateRule(index, { upstream_model: inputValue($event) })"
          />
          <p
            v-if="showValidation && hasError(index, 'upstreamModelRequired')"
            class="mt-1 text-xs text-red-600 dark:text-red-400"
            role="alert"
          >
            {{ t("admin.groups.compositeRoutes.rules.errors.upstreamModelRequired") }}
          </p>
        </div>
      </div>

      <div class="space-y-3 border-t border-gray-200 pt-3 dark:border-dark-600">
        <div class="text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.compositeRoutes.rules.conditions") }}
        </div>

        <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <div v-for="condition in booleanConditions" :key="condition.key">
            <label :for="`${idPrefix}-${index}-${condition.key}`" class="input-label">
              {{ condition.label }}
            </label>
            <Select
              :id="`${idPrefix}-${index}-${condition.key}`"
              :model-value="triStateFromCondition(rule.conditions[condition.key])"
              :options="triStateOptions"
              :searchable="false"
              :disabled="disabled"
              @update:model-value="updateBooleanCondition(index, condition.key, $event as CompositeRuleTriState)"
            />
          </div>
        </div>

        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <div>
            <label :for="`${idPrefix}-${index}-min-tokens`" class="input-label">
              {{ t("admin.groups.compositeRoutes.rules.minInputTokens") }}
            </label>
            <input
              :id="`${idPrefix}-${index}-min-tokens`"
              :value="rule.conditions.min_input_tokens || ''"
              type="number"
              min="0"
              step="1"
              :class="['input', hasError(index, 'minGreaterThanMax') && 'input-error']"
              :placeholder="t('admin.groups.compositeRoutes.rules.tokensPlaceholder')"
              :disabled="disabled"
              :data-testid="`composite-rule-min-tokens-${index}`"
              @input="updateConditions(index, { min_input_tokens: Number(inputValue($event)) })"
            />
          </div>
          <div>
            <label :for="`${idPrefix}-${index}-max-tokens`" class="input-label">
              {{ t("admin.groups.compositeRoutes.rules.maxInputTokens") }}
            </label>
            <input
              :id="`${idPrefix}-${index}-max-tokens`"
              :value="rule.conditions.max_input_tokens || ''"
              type="number"
              min="0"
              step="1"
              :class="['input', hasError(index, 'minGreaterThanMax') && 'input-error']"
              :placeholder="t('admin.groups.compositeRoutes.rules.tokensPlaceholder')"
              :disabled="disabled"
              :data-testid="`composite-rule-max-tokens-${index}`"
              @input="updateConditions(index, { max_input_tokens: Number(inputValue($event)) })"
            />
          </div>
          <p
            v-if="hasError(index, 'minGreaterThanMax')"
            class="text-xs text-red-600 dark:text-red-400 sm:col-span-2"
            role="alert"
            :data-testid="`composite-rule-token-error-${index}`"
          >
            {{ t("admin.groups.compositeRoutes.rules.errors.minGreaterThanMax") }}
          </p>
        </div>

        <fieldset :disabled="disabled">
          <legend class="input-label">
            {{ t("admin.groups.compositeRoutes.rules.clients") }}
          </legend>
          <div class="flex flex-wrap gap-x-4 gap-y-2">
            <label
              v-for="client in COMPOSITE_ROUTE_CLIENTS"
              :key="client"
              class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300"
            >
              <input
                type="checkbox"
                class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-600 dark:bg-dark-700"
                :checked="(rule.conditions.clients ?? []).includes(client)"
                :data-testid="`composite-rule-client-${index}-${client}`"
                @change="toggleClient(index, client)"
              />
              {{ clientLabel(client) }}
            </label>
          </div>
          <p class="input-hint">
            {{ t("admin.groups.compositeRoutes.rules.clientsHint") }}
          </p>
        </fieldset>

        <div>
          <label :for="`${idPrefix}-${index}-keywords`" class="input-label">
            {{ t("admin.groups.compositeRoutes.rules.keywords") }}
          </label>
          <div
            class="flex min-h-[38px] flex-wrap items-center gap-1 rounded-lg border border-gray-200 bg-white px-2 py-1 focus-within:border-primary-500 dark:border-dark-500 dark:bg-dark-700"
          >
            <span
              v-for="keyword in rule.conditions.keywords ?? []"
              :key="keyword"
              class="inline-flex items-center gap-1 rounded bg-primary-50 px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/20 dark:text-primary-300"
            >
              {{ keyword }}
              <button
                type="button"
                class="rounded hover:text-primary-900 dark:hover:text-primary-100"
                :aria-label="`${t('admin.groups.compositeRoutes.rules.removeKeyword')} ${keyword}`"
                :disabled="disabled"
                @click="removeKeyword(index, keyword)"
              >
                <Icon name="x" size="xs" />
              </button>
            </span>
            <input
              :id="`${idPrefix}-${index}-keywords`"
              v-model="keywordDrafts[index]"
              type="text"
              autocomplete="off"
              class="min-w-[140px] flex-1 border-0 bg-transparent px-1 py-0.5 text-sm text-gray-900 focus:outline-none focus:ring-0 dark:text-white"
              :maxlength="MAX_COMPOSITE_RULE_KEYWORD_LENGTH"
              :placeholder="t('admin.groups.compositeRoutes.rules.keywordsPlaceholder')"
              :disabled="disabled || (rule.conditions.keywords?.length ?? 0) >= MAX_COMPOSITE_RULE_KEYWORDS"
              :data-testid="`composite-rule-keyword-draft-${index}`"
              @keydown="handleKeywordKeydown(index, $event)"
              @blur="commitKeywordDraft(index)"
            />
          </div>
          <p class="input-hint">
            {{ t("admin.groups.compositeRoutes.rules.keywordsHint", { max: MAX_COMPOSITE_RULE_KEYWORDS }) }}
          </p>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive } from "vue";
import { useI18n } from "vue-i18n";
import Icon from "@/components/icons/Icon.vue";
import Select from "@/components/common/Select.vue";
import Toggle from "@/components/common/Toggle.vue";
import type {
  CompositeRouteClient,
  CompositeRouteConditions,
  CompositeRouteRule,
} from "@/types";
import {
  COMPOSITE_ROUTE_CLIENTS,
  COMPOSITE_ROUTE_CLIENT_LABEL_KEYS,
  MAX_COMPOSITE_ROUTE_RULES,
  MAX_COMPOSITE_RULE_KEYWORDS,
  MAX_COMPOSITE_RULE_KEYWORD_LENGTH,
  MAX_COMPOSITE_RULE_NAME_LENGTH,
  MAX_COMPOSITE_RULE_UPSTREAM_MODEL_LENGTH,
  compositeRouteRuleErrors,
  conditionFromTriState,
  createCompositeRouteRule,
  normalizeCompositeRouteConditions,
  normalizeCompositeRuleKeywords,
  triStateFromCondition,
  type CompositeRuleBooleanCondition,
  type CompositeRuleError,
  type CompositeRuleTriState,
} from "@/views/admin/groupsCompositeRouteRules";

type RulePlatform = CompositeRouteRule["target_platform"];

const props = withDefaults(
  defineProps<{
    modelValue: CompositeRouteRule[];
    platformOptions: Array<{ value: string; label: string }>;
    /** 新增规则时默认使用的目标平台（通常为路由自身的目标平台） */
    defaultPlatform: RulePlatform;
    disabled?: boolean;
    /** 为 true 时显示「上游模型必填」等提交时校验提示；min > max 始终即时提示 */
    showValidation?: boolean;
    idPrefix?: string;
  }>(),
  {
    disabled: false,
    showValidation: false,
    idPrefix: "composite-rule",
  },
);

const emit = defineEmits<{
  "update:modelValue": [value: CompositeRouteRule[]];
}>();

const { t } = useI18n();

const iconButtonClass =
  "rounded p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 disabled:cursor-not-allowed disabled:opacity-40 disabled:hover:bg-transparent disabled:hover:text-gray-500 dark:hover:bg-dark-700 dark:hover:text-primary-400";

const keywordDrafts = reactive<Record<number, string>>({});

const booleanConditions = computed<Array<{ key: CompositeRuleBooleanCondition; label: string }>>(() => [
  { key: "has_images", label: t("admin.groups.compositeRoutes.rules.hasImages") },
  { key: "has_tools", label: t("admin.groups.compositeRoutes.rules.hasTools") },
  { key: "reasoning", label: t("admin.groups.compositeRoutes.rules.reasoning") },
]);

const triStateOptions = computed(() => [
  { value: "any", label: t("admin.groups.compositeRoutes.rules.triState.any") },
  { value: "yes", label: t("admin.groups.compositeRoutes.rules.triState.yes") },
  { value: "no", label: t("admin.groups.compositeRoutes.rules.triState.no") },
]);

const clientLabel = (client: CompositeRouteClient) =>
  t(`admin.groups.compositeRoutes.clients.${COMPOSITE_ROUTE_CLIENT_LABEL_KEYS[client]}`);

const ruleErrors = computed(() => props.modelValue.map(compositeRouteRuleErrors));

const hasError = (index: number, error: CompositeRuleError) =>
  ruleErrors.value[index]?.includes(error) ?? false;

const inputValue = (event: Event) => (event.target as HTMLInputElement).value;

const clearKeywordDrafts = () => {
  for (const key of Object.keys(keywordDrafts)) delete keywordDrafts[Number(key)];
};

const emitRules = (rules: CompositeRouteRule[]) => emit("update:modelValue", rules);

const updateRule = (index: number, patch: Partial<CompositeRouteRule>) => {
  emitRules(
    props.modelValue.map((rule, current) => (current === index ? { ...rule, ...patch } : rule)),
  );
};

const updateConditions = (index: number, patch: CompositeRouteConditions) => {
  const rule = props.modelValue[index];
  if (!rule) return;
  updateRule(index, {
    conditions: normalizeCompositeRouteConditions({ ...rule.conditions, ...patch }),
  });
};

const updateBooleanCondition = (
  index: number,
  key: CompositeRuleBooleanCondition,
  value: CompositeRuleTriState,
) => {
  const patch: CompositeRouteConditions = {};
  patch[key] = conditionFromTriState(value);
  updateConditions(index, patch);
};

const toggleClient = (index: number, client: CompositeRouteClient) => {
  const current = props.modelValue[index]?.conditions.clients ?? [];
  const clients = current.includes(client)
    ? current.filter((existing) => existing !== client)
    : [...current, client];
  updateConditions(index, { clients });
};

const addRule = () => {
  if (props.modelValue.length >= MAX_COMPOSITE_ROUTE_RULES) return;
  emitRules([...props.modelValue, createCompositeRouteRule(props.defaultPlatform)]);
};

const removeRule = (index: number) => {
  clearKeywordDrafts();
  emitRules(props.modelValue.filter((_, current) => current !== index));
};

const moveRule = (index: number, offset: -1 | 1) => {
  const target = index + offset;
  if (target < 0 || target >= props.modelValue.length) return;
  const next = [...props.modelValue];
  [next[index], next[target]] = [next[target], next[index]];
  clearKeywordDrafts();
  emitRules(next);
};

// 关键词可能包含空格，只按逗号或换行拆分。
const commitKeywordDraft = (index: number) => {
  const draft = keywordDrafts[index] ?? "";
  keywordDrafts[index] = "";
  const rule = props.modelValue[index];
  if (!rule) return;
  const parts = draft.split(/[,，\n]+/);
  const current = rule.conditions.keywords ?? [];
  const keywords = normalizeCompositeRuleKeywords([...current, ...parts]).slice(
    0,
    MAX_COMPOSITE_RULE_KEYWORDS,
  );
  if (keywords.length !== current.length) updateConditions(index, { keywords });
};

const removeKeyword = (index: number, keyword: string) => {
  const current = props.modelValue[index]?.conditions.keywords ?? [];
  updateConditions(index, { keywords: current.filter((existing) => existing !== keyword) });
};

const handleKeywordKeydown = (index: number, event: KeyboardEvent) => {
  if (event.isComposing) return;
  if (event.key === "Enter" || event.key === ",") {
    event.preventDefault();
    commitKeywordDraft(index);
    return;
  }
  const keywords = props.modelValue[index]?.conditions.keywords ?? [];
  if (event.key === "Backspace" && !keywordDrafts[index] && keywords.length > 0) {
    removeKeyword(index, keywords[keywords.length - 1]);
  }
};
</script>
