<template>
  <div class="space-y-3" data-testid="composite-route-decision-details">
    <div
      v-if="decision.rule_index != null"
      class="flex flex-wrap items-center gap-1.5 text-gray-700 dark:text-gray-300"
    >
      <span>{{ t("admin.groups.compositeRoutes.decision.rule") }}:</span>
      <span class="badge badge-primary" data-testid="composite-decision-rule">
        {{ ruleLabel(decision.rule_index, decision.rule_name) }}
      </span>
      <span v-if="decision.sticky" class="badge badge-purple">
        {{ t("admin.groups.compositeRoutes.decision.sticky") }}
      </span>
    </div>

    <div v-if="decision.features">
      <div class="mb-1.5 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
        {{ t("admin.groups.compositeRoutes.decision.features") }}
      </div>
      <dl class="grid grid-cols-2 gap-x-3 gap-y-1 text-xs">
        <template v-for="item in featureItems" :key="item.label">
          <dt class="text-gray-500 dark:text-gray-400">{{ item.label }}</dt>
          <dd class="text-gray-900 dark:text-white">{{ item.value }}</dd>
        </template>
      </dl>
      <div v-if="decision.features.last_user_text" class="mt-1.5 text-xs">
        <div class="text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.compositeRoutes.decision.lastUserText") }}
        </div>
        <div
          class="mt-0.5 max-h-20 overflow-y-auto whitespace-pre-wrap break-all rounded border border-gray-200 bg-white px-2 py-1 text-gray-700 dark:border-dark-600 dark:bg-dark-900 dark:text-gray-300"
        >
          {{ decision.features.last_user_text }}
        </div>
      </div>
    </div>

    <div v-if="decision.trace?.length">
      <div class="mb-1.5 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
        {{ t("admin.groups.compositeRoutes.decision.trace") }}
      </div>
      <div class="overflow-x-auto rounded border border-gray-200 dark:border-dark-600">
        <table class="min-w-full divide-y divide-gray-200 text-xs dark:divide-dark-600">
          <thead class="whitespace-nowrap bg-white text-left font-medium text-gray-500 dark:bg-dark-900 dark:text-gray-400">
            <tr>
              <th class="px-2 py-1.5">{{ t("admin.groups.compositeRoutes.decision.traceRule") }}</th>
              <th class="px-2 py-1.5">{{ t("admin.groups.compositeRoutes.decision.traceTarget") }}</th>
              <th class="px-2 py-1.5 text-center">{{ t("admin.groups.compositeRoutes.decision.traceMatched") }}</th>
              <th class="px-2 py-1.5 text-center">{{ t("admin.groups.compositeRoutes.decision.traceAvailable") }}</th>
              <th class="px-2 py-1.5 text-center">{{ t("admin.groups.compositeRoutes.decision.traceSelected") }}</th>
              <th class="px-2 py-1.5">{{ t("admin.groups.compositeRoutes.decision.traceReason") }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-700 dark:bg-dark-900">
            <tr
              v-for="(entry, index) in decision.trace"
              :key="index"
              :class="entry.selected && 'bg-primary-50/60 dark:bg-primary-900/10'"
            >
              <td class="whitespace-nowrap px-2 py-1.5 text-gray-900 dark:text-white">
                {{ ruleLabel(entry.rule_index, entry.rule_name) }}
              </td>
              <td class="whitespace-nowrap px-2 py-1.5 text-gray-700 dark:text-gray-300">
                <div>{{ formatPlatform(entry.target_platform) }}</div>
                <div class="text-gray-500 dark:text-gray-400">
                  {{ entry.upstream_model || "—" }}
                </div>
              </td>
              <td v-for="flag in traceFlags(entry)" :key="flag.key" class="px-2 py-1.5 text-center">
                <Icon
                  :name="flag.value ? 'check' : 'x'"
                  size="sm"
                  :class="[
                    'inline-block',
                    flag.value ? 'text-green-600 dark:text-green-400' : 'text-gray-400 dark:text-gray-500',
                  ]"
                  aria-hidden="true"
                />
                <span class="sr-only">{{ yesNo(flag.value) }}</span>
              </td>
              <td class="min-w-[12rem] break-words px-2 py-1.5 text-gray-500 dark:text-gray-400">
                {{ entry.reason || "—" }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";
import Icon from "@/components/icons/Icon.vue";
import { platformLabel } from "@/utils/platformColors";
import type {
  CompositeRouteClient,
  CompositeRouteDecision,
  CompositeRouteTraceEntry,
} from "@/types";
import { COMPOSITE_ROUTE_CLIENT_LABEL_KEYS } from "@/views/admin/groupsCompositeRouteRules";

const props = defineProps<{
  decision: CompositeRouteDecision;
}>();

const { t } = useI18n();

const yesNo = (value: boolean) =>
  value
    ? t("admin.groups.compositeRoutes.decision.yes")
    : t("admin.groups.compositeRoutes.decision.no");

const formatPlatform = (platform: string) => {
  if (!platform) return "—";
  return t(`admin.groups.platforms.${platform}`, platformLabel(platform));
};

const formatClient = (client: string) => {
  const key = COMPOSITE_ROUTE_CLIENT_LABEL_KEYS[client as CompositeRouteClient];
  return key ? t(`admin.groups.compositeRoutes.clients.${key}`) : client || "—";
};

const ruleLabel = (ruleIndex: number, ruleName?: string) => {
  if (ruleIndex < 0) return t("admin.groups.compositeRoutes.decision.defaultTarget");
  const label = t("admin.groups.compositeRoutes.rules.ruleLabel", { index: ruleIndex + 1 });
  return ruleName ? `${label} · ${ruleName}` : label;
};

const featureItems = computed(() => {
  const features = props.decision.features;
  if (!features) return [];
  return [
    { label: t("admin.groups.compositeRoutes.decision.images"), value: yesNo(features.has_images) },
    { label: t("admin.groups.compositeRoutes.decision.tools"), value: yesNo(features.has_tools) },
    { label: t("admin.groups.compositeRoutes.decision.reasoning"), value: yesNo(features.reasoning) },
    {
      label: t("admin.groups.compositeRoutes.decision.inputTokens"),
      value: features.input_tokens.toLocaleString(),
    },
    { label: t("admin.groups.compositeRoutes.decision.client"), value: formatClient(features.client) },
  ];
});

const traceFlags = (entry: CompositeRouteTraceEntry) => [
  { key: "matched", value: entry.matched },
  { key: "available", value: entry.available },
  { key: "selected", value: entry.selected },
];
</script>
