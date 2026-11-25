import type {
  CompositeRouteClient,
  CompositeRouteConditions,
  CompositeRouteEndpoint,
  CompositeRouteRule,
  GroupPlatform,
} from "@/types";

export const MAX_COMPOSITE_ROUTE_RULES = 20;
export const MAX_COMPOSITE_RULE_KEYWORDS = 20;
export const MAX_COMPOSITE_RULE_KEYWORD_LENGTH = 100;
export const MAX_COMPOSITE_RULE_NAME_LENGTH = 64;
export const MAX_COMPOSITE_RULE_UPSTREAM_MODEL_LENGTH = 200;

// 只有这些端点会在网关里解析请求正文；其它端点保存非空 rules 时后端返回 400。
const compositeRuleEndpoints: readonly CompositeRouteEndpoint[] = [
  "any",
  "messages",
  "responses",
  "chat_completions",
];

export const COMPOSITE_ROUTE_CLIENTS: readonly CompositeRouteClient[] = [
  "claude_code",
  "codex",
  "gemini_cli",
  "other",
];

// i18n 键：admin.groups.compositeRoutes.clients.<key>
export const COMPOSITE_ROUTE_CLIENT_LABEL_KEYS: Record<CompositeRouteClient, string> = {
  claude_code: "claudeCode",
  codex: "codex",
  gemini_cli: "geminiCli",
  other: "other",
};

export type CompositeRuleTriState = "any" | "yes" | "no";
export type CompositeRuleBooleanCondition = "has_images" | "has_tools" | "reasoning";
export type CompositeRuleError = "upstreamModelRequired" | "minGreaterThanMax";

export const compositeRouteSupportsRules = (endpoint: CompositeRouteEndpoint) =>
  compositeRuleEndpoints.includes(endpoint);

export const createCompositeRouteRule = (
  targetPlatform: Exclude<GroupPlatform, "composite">,
): CompositeRouteRule => ({
  name: "",
  enabled: true,
  target_platform: targetPlatform,
  upstream_model: "",
  conditions: {},
});

export const triStateFromCondition = (
  value: boolean | null | undefined,
): CompositeRuleTriState => {
  if (value === true) return "yes";
  if (value === false) return "no";
  return "any";
};

export const conditionFromTriState = (
  value: CompositeRuleTriState,
): boolean | undefined => {
  if (value === "yes") return true;
  if (value === "no") return false;
  return undefined;
};

const toTokenLimit = (value: unknown): number => {
  const parsed = Math.floor(Number(value));
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
};

/** 关键词去首尾空白、去空、按小写去重，保留首次出现的写法。 */
export const normalizeCompositeRuleKeywords = (keywords: readonly string[]) => {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const raw of keywords) {
    const keyword = raw.trim();
    if (!keyword) continue;
    const key = keyword.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    result.push(keyword);
  }
  return result;
};

/**
 * 只保留已设置的条件：三态未设置、token 上下限为 0、clients/keywords 为空都视为「不限」并省略字段。
 */
export const normalizeCompositeRouteConditions = (
  conditions: CompositeRouteConditions | null | undefined,
): CompositeRouteConditions => {
  const source = conditions ?? {};
  const result: CompositeRouteConditions = {};
  const booleanKeys: CompositeRuleBooleanCondition[] = ["has_images", "has_tools", "reasoning"];
  for (const key of booleanKeys) {
    const value = source[key];
    if (typeof value === "boolean") result[key] = value;
  }
  const minTokens = toTokenLimit(source.min_input_tokens);
  if (minTokens > 0) result.min_input_tokens = minTokens;
  const maxTokens = toTokenLimit(source.max_input_tokens);
  if (maxTokens > 0) result.max_input_tokens = maxTokens;
  const clients = COMPOSITE_ROUTE_CLIENTS.filter((client) =>
    (source.clients ?? []).includes(client),
  );
  if (clients.length > 0) result.clients = clients;
  const keywords = normalizeCompositeRuleKeywords(source.keywords ?? []);
  if (keywords.length > 0) result.keywords = keywords;
  return result;
};

/** 回填编辑表单：深拷贝并容忍后端返回的 null / 缺省字段。 */
export const cloneCompositeRouteRules = (
  rules: readonly CompositeRouteRule[] | null | undefined,
): CompositeRouteRule[] =>
  (rules ?? []).map((rule) => ({
    name: rule.name ?? "",
    enabled: rule.enabled !== false,
    target_platform: rule.target_platform,
    upstream_model: rule.upstream_model ?? "",
    conditions: normalizeCompositeRouteConditions(rule.conditions),
  }));

/** 生成提交给 API 的 rules：去除名称/上游模型首尾空白，空名称省略。 */
export const normalizeCompositeRouteRules = (
  rules: readonly CompositeRouteRule[],
): CompositeRouteRule[] =>
  rules.map((rule) => {
    const name = (rule.name ?? "").trim();
    return {
      ...(name ? { name } : {}),
      enabled: rule.enabled,
      target_platform: rule.target_platform,
      upstream_model: rule.upstream_model.trim(),
      conditions: normalizeCompositeRouteConditions(rule.conditions),
    };
  });

export const compositeRouteRuleErrors = (rule: CompositeRouteRule): CompositeRuleError[] => {
  const errors: CompositeRuleError[] = [];
  if (!rule.upstream_model.trim()) errors.push("upstreamModelRequired");
  const minTokens = toTokenLimit(rule.conditions?.min_input_tokens);
  const maxTokens = toTokenLimit(rule.conditions?.max_input_tokens);
  if (minTokens > 0 && maxTokens > 0 && minTokens > maxTokens) {
    errors.push("minGreaterThanMax");
  }
  return errors;
};

export const compositeRouteRulesValid = (rules: readonly CompositeRouteRule[]) =>
  rules.length <= MAX_COMPOSITE_ROUTE_RULES &&
  rules.every((rule) => compositeRouteRuleErrors(rule).length === 0);
