package service

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	compositeRouteMaxRules            = 20
	compositeRouteRuleNameMaxLen      = 64
	compositeRouteRuleUpstreamMaxLen  = 200
	compositeRouteRuleMaxKeywords     = 20
	compositeRouteRuleKeywordMaxRunes = 100

	compositeRouteInvalidRulesReason = "COMPOSITE_ROUTE_INVALID_RULES"
)

var compositeRouteRuleClients = map[string]struct{}{
	domain.CompositeRouteClientClaudeCode: {},
	domain.CompositeRouteClientCodex:      {},
	domain.CompositeRouteClientGeminiCLI:  {},
	domain.CompositeRouteClientOther:      {},
}

// compositeRouteRulesEndpointAllowed reports whether a route scoped to endpoint
// receives request bodies the smart rules can inspect.
func compositeRouteRulesEndpointAllowed(endpoint string) bool {
	switch endpoint {
	case CompositeRouteEndpointAny,
		CompositeRouteEndpointMessages,
		CompositeRouteEndpointResponses,
		CompositeRouteEndpointChatCompletions:
		return true
	default:
		return false
	}
}

func invalidCompositeRouteRules(format string, args ...any) error {
	return infraerrors.BadRequest(compositeRouteInvalidRulesReason, fmt.Sprintf(format, args...))
}

// normalizeCompositeRouteRules validates rules for a route on endpoint and
// returns them trimmed, with clients/keywords de-duplicated. The result is
// never nil so storage and API responses always carry an array.
func normalizeCompositeRouteRules(rules []CompositeRouteRule, endpoint string) ([]CompositeRouteRule, error) {
	if len(rules) > compositeRouteMaxRules {
		return nil, invalidCompositeRouteRules("rules: at most %d rules are allowed", compositeRouteMaxRules)
	}
	if len(rules) > 0 && !compositeRouteRulesEndpointAllowed(endpoint) {
		return nil, invalidCompositeRouteRules("rules require route endpoint any, messages, responses or chat_completions")
	}
	out := make([]CompositeRouteRule, 0, len(rules))
	for i, rule := range rules {
		normalized, err := normalizeCompositeRouteRule(rule)
		if err != nil {
			return nil, invalidCompositeRouteRules("rules[%d]: %s", i, err.Error())
		}
		out = append(out, normalized)
	}
	return out, nil
}

func normalizeCompositeRouteRule(rule CompositeRouteRule) (CompositeRouteRule, error) {
	rule.Name = strings.TrimSpace(rule.Name)
	if utf8.RuneCountInString(rule.Name) > compositeRouteRuleNameMaxLen {
		return rule, fmt.Errorf("name must be at most %d characters", compositeRouteRuleNameMaxLen)
	}
	rule.TargetPlatform = strings.TrimSpace(rule.TargetPlatform)
	if !isConcreteRequestPlatform(rule.TargetPlatform) {
		return rule, fmt.Errorf("target_platform must be a concrete provider")
	}
	rule.UpstreamModel = strings.TrimSpace(rule.UpstreamModel)
	if rule.UpstreamModel == "" {
		return rule, fmt.Errorf("upstream_model is required")
	}
	if utf8.RuneCountInString(rule.UpstreamModel) > compositeRouteRuleUpstreamMaxLen {
		return rule, fmt.Errorf("upstream_model must be at most %d characters", compositeRouteRuleUpstreamMaxLen)
	}
	conditions, err := normalizeCompositeRouteConditions(rule.Conditions)
	if err != nil {
		return rule, err
	}
	rule.Conditions = conditions
	return rule, nil
}

func normalizeCompositeRouteConditions(c CompositeRouteConditions) (CompositeRouteConditions, error) {
	if c.MinInputTokens < 0 {
		return c, fmt.Errorf("conditions.min_input_tokens must be >= 0")
	}
	if c.MaxInputTokens < 0 {
		return c, fmt.Errorf("conditions.max_input_tokens must be >= 0")
	}
	if c.MinInputTokens > 0 && c.MaxInputTokens > 0 && c.MinInputTokens > c.MaxInputTokens {
		return c, fmt.Errorf("conditions.min_input_tokens must be <= max_input_tokens")
	}

	var clients []string
	seenClients := make(map[string]struct{}, len(c.Clients))
	for _, client := range c.Clients {
		client = strings.ToLower(strings.TrimSpace(client))
		if _, ok := compositeRouteRuleClients[client]; !ok {
			return c, fmt.Errorf("conditions.clients contains unsupported client %q (allowed: claude_code, codex, gemini_cli, other)", client)
		}
		if _, dup := seenClients[client]; dup {
			continue
		}
		seenClients[client] = struct{}{}
		clients = append(clients, client)
	}
	c.Clients = clients

	var keywords []string
	seenKeywords := make(map[string]struct{}, len(c.Keywords))
	for _, keyword := range c.Keywords {
		keyword = strings.TrimSpace(keyword)
		runes := utf8.RuneCountInString(keyword)
		if runes == 0 || runes > compositeRouteRuleKeywordMaxRunes {
			return c, fmt.Errorf("conditions.keywords entries must be 1-%d characters", compositeRouteRuleKeywordMaxRunes)
		}
		key := strings.ToLower(keyword)
		if _, dup := seenKeywords[key]; dup {
			continue
		}
		seenKeywords[key] = struct{}{}
		keywords = append(keywords, keyword)
	}
	if len(keywords) > compositeRouteRuleMaxKeywords {
		return c, fmt.Errorf("conditions.keywords allows at most %d entries", compositeRouteRuleMaxKeywords)
	}
	c.Keywords = keywords
	return c, nil
}

// compositeRouteConditionsMismatch returns why features fail conditions, or ""
// when every set condition holds.
func compositeRouteConditionsMismatch(c CompositeRouteConditions, f CompositeRouteFeatures) string {
	if reason := compositeRouteCapabilityMismatch(c, f); reason != "" {
		return reason
	}
	if c.HasTools != nil && *c.HasTools != f.HasTools {
		return fmt.Sprintf("has_tools is %t", f.HasTools)
	}
	if c.Reasoning != nil && *c.Reasoning != f.Reasoning {
		return fmt.Sprintf("reasoning is %t", f.Reasoning)
	}
	if len(c.Clients) > 0 && !containsString(c.Clients, f.Client) {
		return fmt.Sprintf("client %s not in clients", f.Client)
	}
	if len(c.Keywords) > 0 && !compositeRouteKeywordsMatch(c.Keywords, f.lastUserText) {
		return "no keyword in last user message"
	}
	return ""
}

// compositeRouteCapabilityMismatch checks only the capability conditions
// (has_images, input token bounds) that must keep holding for a session to stay
// on its earlier choice.
func compositeRouteCapabilityMismatch(c CompositeRouteConditions, f CompositeRouteFeatures) string {
	if c.HasImages != nil && *c.HasImages != f.HasImages {
		return fmt.Sprintf("has_images is %t", f.HasImages)
	}
	if c.MinInputTokens > 0 && f.InputTokens < c.MinInputTokens {
		return fmt.Sprintf("input_tokens %d < min_input_tokens %d", f.InputTokens, c.MinInputTokens)
	}
	if c.MaxInputTokens > 0 && f.InputTokens > c.MaxInputTokens {
		return fmt.Sprintf("input_tokens %d > max_input_tokens %d", f.InputTokens, c.MaxInputTokens)
	}
	return ""
}

// compositeRouteCapabilityUpgrade reports whether the freshly preferred
// conditions demand a capability (images or a large context) that the sticky
// choice does not, which forces a session to switch targets.
func compositeRouteCapabilityUpgrade(preferred, sticky *CompositeRouteConditions) bool {
	if preferred == nil {
		return false
	}
	if preferred.HasImages != nil && *preferred.HasImages && (sticky == nil || sticky.HasImages == nil || !*sticky.HasImages) {
		return true
	}
	return preferred.MinInputTokens > 0 && (sticky == nil || sticky.MinInputTokens <= 0)
}

func compositeRouteKeywordsMatch(keywords []string, text string) bool {
	if text == "" {
		return false
	}
	lower := strings.ToLower(text)
	for _, keyword := range keywords {
		if keyword != "" && strings.Contains(lower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}
