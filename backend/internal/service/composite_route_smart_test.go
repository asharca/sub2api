package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type compositeStickyStoreStub struct {
	values map[string]string
	sets   int
}

func newCompositeStickyStoreStub() *compositeStickyStoreStub {
	return &compositeStickyStoreStub{values: map[string]string{}}
}

func compositeStickyStubKey(groupID, routeID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%d:%s", groupID, routeID, sessionHash)
}

func (s *compositeStickyStoreStub) GetCompositeRouteSticky(_ context.Context, groupID, routeID int64, sessionHash string) (string, bool, error) {
	value, ok := s.values[compositeStickyStubKey(groupID, routeID, sessionHash)]
	return value, ok, nil
}

func (s *compositeStickyStoreStub) SetCompositeRouteSticky(_ context.Context, groupID, routeID int64, sessionHash, target string, ttl time.Duration) error {
	if ttl != compositeRouteStickyTTL {
		return errors.New("unexpected ttl")
	}
	s.sets++
	s.values[compositeStickyStubKey(groupID, routeID, sessionHash)] = target
	return nil
}

func (s *compositeStickyStoreStub) seed(groupID, routeID int64, sessionID, target string) {
	s.values[compositeStickyStubKey(groupID, routeID, compositeRouteStickySessionHash(sessionID))] = target
}

func (s *compositeStickyStoreStub) get(groupID, routeID int64, sessionID string) string {
	return s.values[compositeStickyStubKey(groupID, routeID, compositeRouteStickySessionHash(sessionID))]
}

// smartAutoRoute is an "auto" route defaulting to Claude Sonnet with a vision
// rule (Gemini), a reasoning rule (OpenAI) and a translation keyword rule.
func smartAutoRoute() CompositeModelRoute {
	return CompositeModelRoute{
		ID:              5,
		GroupID:         7,
		PublicModel:     "auto",
		MatchType:       CompositeRouteMatchExact,
		TargetPlatform:  PlatformAnthropic,
		UpstreamModel:   "claude-sonnet-4-6",
		Endpoint:        CompositeRouteEndpointAny,
		Priority:        100,
		Enabled:         true,
		SessionAffinity: true,
		Rules: []CompositeRouteRule{
			{Name: "vision", Enabled: true, TargetPlatform: PlatformGemini, UpstreamModel: "gemini-2.5-pro", Conditions: CompositeRouteConditions{HasImages: new(true)}},
			{Name: "think", Enabled: true, TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Conditions: CompositeRouteConditions{Reasoning: new(true)}},
			{Name: "translate", Enabled: true, TargetPlatform: PlatformDeepseek, UpstreamModel: "deepseek-chat", Conditions: CompositeRouteConditions{Keywords: []string{"翻译"}}},
		},
	}
}

func smartResolver(route CompositeModelRoute, store CompositeRouteStickyStore, unavailable ...string) *CompositeRouteResolver {
	resolver := NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{route}})
	resolver.SetTargetAvailabilityChecker(func(_ context.Context, _ int64, platform, _ string) (bool, error) {
		for _, p := range unavailable {
			if p == platform {
				return false, nil
			}
		}
		return true, nil
	})
	if store != nil {
		resolver.SetStickyStore(store)
	}
	return resolver
}

func messagesContent(body string, sessionID string) *CompositeRouteRequestContent {
	return &CompositeRouteRequestContent{
		FeatureEndpoint: CompositeRouteEndpointMessages,
		Body:            []byte(body),
		UserAgent:       "claude-cli/2.1.0 (external, cli)",
		SessionID:       func() string { return sessionID },
	}
}

const (
	imageMessagesBody     = `{"model":"auto","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"iVBORw0KGgo="}},{"type":"text","text":"what is this"}]}]}`
	thinkingMessagesBody  = `{"model":"auto","thinking":{"type":"enabled","budget_tokens":2048},"messages":[{"role":"user","content":"solve it"}]}`
	translateMessagesBody = `{"model":"auto","messages":[{"role":"user","content":"请帮我翻译这段话"}]}`
	plainMessagesBody     = `{"model":"auto","messages":[{"role":"user","content":"hello"}]}`
)

func TestCompositeSmartRulesSelectFirstMatchingRuleInOrder(t *testing.T) {
	resolver := smartResolver(smartAutoRoute(), nil)
	body := `{"model":"auto","thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}},{"type":"text","text":"翻译图片"}]}]}`

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(body, ""))

	require.NoError(t, err)
	require.True(t, decision.Matched)
	require.Equal(t, PlatformGemini, decision.TargetPlatform)
	require.Equal(t, "gemini-2.5-pro", decision.UpstreamModel)
	require.NotNil(t, decision.RuleIndex)
	require.Equal(t, 0, *decision.RuleIndex)
	require.Equal(t, "vision", decision.RuleName)
	require.False(t, decision.Sticky)
	require.Len(t, decision.Trace, 4)
	require.Equal(t, []int{0, 1, 2, -1}, []int{decision.Trace[0].RuleIndex, decision.Trace[1].RuleIndex, decision.Trace[2].RuleIndex, decision.Trace[3].RuleIndex})
	require.True(t, decision.Trace[0].Selected)
	require.True(t, decision.Trace[1].Matched)
	require.True(t, decision.Trace[2].Matched)
	require.Equal(t, compositeRouteTraceReasonNotChecked, decision.Trace[1].Reason)
}

func TestCompositeSmartRulesFallBackToRouteTargetWhenNothingMatches(t *testing.T) {
	resolver := smartResolver(smartAutoRoute(), nil)

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(plainMessagesBody, ""))

	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, decision.TargetPlatform)
	require.Equal(t, "claude-sonnet-4-6", decision.UpstreamModel)
	require.Equal(t, -1, *decision.RuleIndex)
	require.Empty(t, decision.RuleName)
	require.NotNil(t, decision.Features)
	require.Equal(t, "hello", decision.Features.LastUserText)
	last := decision.Trace[len(decision.Trace)-1]
	require.Equal(t, -1, last.RuleIndex)
	require.True(t, last.Selected)
	require.False(t, decision.Trace[0].Matched)
	require.Equal(t, "has_images is false", decision.Trace[0].Reason)
}

func TestCompositeSmartRulesDisabledRuleIsSkipped(t *testing.T) {
	route := smartAutoRoute()
	route.Rules[0].Enabled = false
	resolver := smartResolver(route, nil)

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(imageMessagesBody, ""))

	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, decision.TargetPlatform)
	require.Equal(t, compositeRouteTraceReasonDisabled, decision.Trace[0].Reason)
}

func TestCompositeSmartRulesSkipUnavailableTargets(t *testing.T) {
	resolver := smartResolver(smartAutoRoute(), nil, PlatformGemini)

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(imageMessagesBody, ""))

	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, decision.TargetPlatform)
	require.Equal(t, -1, *decision.RuleIndex)
	require.True(t, decision.Trace[0].Matched)
	require.False(t, decision.Trace[0].Available)
	require.Equal(t, compositeRouteTraceReasonUnavailable, decision.Trace[0].Reason)
	require.True(t, decision.Trace[3].Available)
}

func TestCompositeSmartRulesAllUnavailableUsesFirstCandidate(t *testing.T) {
	resolver := smartResolver(smartAutoRoute(), nil, PlatformGemini, PlatformAnthropic)

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(imageMessagesBody, ""))

	require.NoError(t, err)
	require.Equal(t, PlatformGemini, decision.TargetPlatform)
	require.Equal(t, 0, *decision.RuleIndex)
	require.Equal(t, compositeRouteTraceReasonAllUnavailable, decision.Trace[0].Reason)
}

func TestCompositeSmartRulesAvailabilityErrorCountsAsAvailable(t *testing.T) {
	resolver := NewCompositeRouteResolver(compositeRouteRepoStub{routes: []CompositeModelRoute{smartAutoRoute()}})
	resolver.SetTargetAvailabilityChecker(func(context.Context, int64, string, string) (bool, error) {
		return false, errors.New("snapshot unavailable")
	})

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(imageMessagesBody, ""))

	require.NoError(t, err)
	require.Equal(t, PlatformGemini, decision.TargetPlatform)
}

func TestCompositeSmartRulesIgnoredWithoutRequestContent(t *testing.T) {
	resolver := smartResolver(smartAutoRoute(), nil)

	decision, err := resolver.Resolve(context.Background(), 7, "auto", CompositeRouteEndpointMessages)

	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, decision.TargetPlatform)
	require.Equal(t, "claude-sonnet-4-6", decision.UpstreamModel)
	require.Nil(t, decision.RuleIndex)
	require.Nil(t, decision.Features)
	require.Nil(t, decision.Trace)
}

func TestCompositeSmartRulesStickySessionKeepsFirstChoice(t *testing.T) {
	store := newCompositeStickyStoreStub()
	resolver := smartResolver(smartAutoRoute(), store)

	first, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(plainMessagesBody, "sess-1"))
	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, first.TargetPlatform)
	require.False(t, first.Sticky)
	require.Equal(t, "anthropic|claude-sonnet-4-6", store.get(7, 5, "sess-1"))

	// A keyword/reasoning match is not a capability: the session stays put.
	second, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(thinkingMessagesBody, "sess-1"))
	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, second.TargetPlatform)
	require.True(t, second.Sticky)
	require.Equal(t, -1, *second.RuleIndex)
	require.Equal(t, 2, store.sets)

	// Another session is routed on its own.
	other, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(translateMessagesBody, "sess-2"))
	require.NoError(t, err)
	require.Equal(t, PlatformDeepseek, other.TargetPlatform)
	require.False(t, other.Sticky)
}

func TestCompositeSmartRulesCapabilityUpgradeSwitchesStickySession(t *testing.T) {
	store := newCompositeStickyStoreStub()
	store.seed(7, 5, "sess-1", "anthropic|claude-sonnet-4-6")
	resolver := smartResolver(smartAutoRoute(), store)

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(imageMessagesBody, "sess-1"))

	require.NoError(t, err)
	require.Equal(t, PlatformGemini, decision.TargetPlatform)
	require.False(t, decision.Sticky)
	require.Equal(t, "gemini|gemini-2.5-pro", store.get(7, 5, "sess-1"))
}

func TestCompositeSmartRulesStickyRuleLosesCapabilityAndSwitches(t *testing.T) {
	route := smartAutoRoute()
	route.Rules = append(route.Rules, CompositeRouteRule{
		Name: "long", Enabled: true, TargetPlatform: PlatformGemini, UpstreamModel: "gemini-2.5-flash",
		Conditions: CompositeRouteConditions{MinInputTokens: 100000},
	})
	store := newCompositeStickyStoreStub()
	store.seed(7, 5, "sess-1", "gemini|gemini-2.5-flash")
	resolver := smartResolver(route, store)

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(plainMessagesBody, "sess-1"))

	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, decision.TargetPlatform)
	require.False(t, decision.Sticky)
	require.Equal(t, "anthropic|claude-sonnet-4-6", store.get(7, 5, "sess-1"))
}

func TestCompositeSmartRulesStickyTargetRemovedReselects(t *testing.T) {
	store := newCompositeStickyStoreStub()
	store.seed(7, 5, "sess-1", "kimi|kimi-k2")
	resolver := smartResolver(smartAutoRoute(), store)

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(translateMessagesBody, "sess-1"))

	require.NoError(t, err)
	require.Equal(t, PlatformDeepseek, decision.TargetPlatform)
	require.False(t, decision.Sticky)
	require.Equal(t, "deepseek|deepseek-chat", store.get(7, 5, "sess-1"))
}

func TestCompositeSmartRulesStickyTargetUnavailableReselects(t *testing.T) {
	store := newCompositeStickyStoreStub()
	store.seed(7, 5, "sess-1", "openai|gpt-5")
	resolver := smartResolver(smartAutoRoute(), store, PlatformOpenAI)

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(plainMessagesBody, "sess-1"))

	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, decision.TargetPlatform)
	require.False(t, decision.Sticky)
}

func TestCompositeSmartRulesStickyDisabledRouteAffinity(t *testing.T) {
	route := smartAutoRoute()
	route.SessionAffinity = false
	store := newCompositeStickyStoreStub()
	store.seed(7, 5, "sess-1", "anthropic|claude-sonnet-4-6")
	resolver := smartResolver(route, store)

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, messagesContent(translateMessagesBody, "sess-1"))

	require.NoError(t, err)
	require.Equal(t, PlatformDeepseek, decision.TargetPlatform)
	require.Zero(t, store.sets)
}

func TestCompositeSmartRulesCountTokensReadsStickyWithoutWriting(t *testing.T) {
	store := newCompositeStickyStoreStub()
	store.seed(7, 5, "sess-1", "openai|gpt-5")
	resolver := smartResolver(smartAutoRoute(), store)
	content := messagesContent(plainMessagesBody, "sess-1")
	content.StickyReadOnly = true

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointCountTokens, content)
	require.NoError(t, err)
	require.Equal(t, PlatformOpenAI, decision.TargetPlatform)
	require.True(t, decision.Sticky)
	require.Zero(t, store.sets)

	fresh := messagesContent(plainMessagesBody, "sess-new")
	fresh.StickyReadOnly = true
	_, err = resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointCountTokens, fresh)
	require.NoError(t, err)
	require.Zero(t, store.sets)
}

func TestCompositeSmartRulesPreviewNeverTouchesSticky(t *testing.T) {
	store := newCompositeStickyStoreStub()
	store.seed(7, 5, "sess-1", "openai|gpt-5")
	resolver := smartResolver(smartAutoRoute(), store, PlatformDeepseek)
	content := messagesContent(imageMessagesBody, "sess-1")
	content.Preview = true

	decision, err := resolver.ResolveRequest(context.Background(), 7, "auto", CompositeRouteEndpointMessages, content)

	require.NoError(t, err)
	require.Equal(t, PlatformGemini, decision.TargetPlatform)
	require.False(t, decision.Sticky)
	require.Zero(t, store.sets)
	// Preview checks every matched candidate.
	require.True(t, decision.Trace[3].Available)
	require.Empty(t, decision.Trace[3].Reason)
}

func TestExtractCompositeRouteFeaturesMessages(t *testing.T) {
	body := `{"model":"auto","system":"You are helpful","thinking":{"type":"adaptive"},"tools":[{"name":"read","description":"read a file","input_schema":{"type":"object"}}],
	"messages":[
		{"role":"user","content":"Translate the first line"},
		{"role":"assistant","content":[{"type":"tool_use","id":"tu_1","name":"read","input":{"path":"a.txt"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu_1","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"QUJDRA=="}}]}]}
	]}`

	features, ok := ExtractCompositeRouteFeatures(CompositeRouteEndpointMessages, []byte(body), "claude-cli/2.1.0 (external, cli)")

	require.True(t, ok)
	require.True(t, features.HasImages)
	require.True(t, features.HasTools)
	require.True(t, features.Reasoning)
	require.Positive(t, features.InputTokens)
	require.Equal(t, domain.CompositeRouteClientClaudeCode, features.Client)
	// The latest user turn only carries a tool_result, so keywords read the
	// most recent user message that has text.
	require.Equal(t, "Translate the first line", features.LastUserText)
}

func TestExtractCompositeRouteFeaturesMessagesThinkingDisabled(t *testing.T) {
	features, ok := ExtractCompositeRouteFeatures(CompositeRouteEndpointMessages, []byte(`{"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"hi"}]}`), "")
	require.True(t, ok)
	require.False(t, features.Reasoning)
	require.False(t, features.HasTools)
	require.False(t, features.HasImages)
	require.Equal(t, domain.CompositeRouteClientOther, features.Client)
}

func TestExtractCompositeRouteFeaturesChatCompletions(t *testing.T) {
	body := `{"model":"auto","reasoning_effort":"high","tools":[{"type":"function","function":{"name":"lookup"}}],
	"messages":[
		{"role":"system","content":"be brief"},
		{"role":"user","content":[{"type":"text","text":"Describe"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]},
		{"role":"assistant","content":"ok"},
		{"role":"tool","tool_call_id":"c1","content":"result"}
	]}`

	features, ok := ExtractCompositeRouteFeatures(CompositeRouteEndpointChatCompletions, []byte(body), "")

	require.True(t, ok)
	require.True(t, features.HasImages)
	require.True(t, features.HasTools)
	require.True(t, features.Reasoning)
	require.Equal(t, "Describe", features.LastUserText)

	noReasoning, ok := ExtractCompositeRouteFeatures(CompositeRouteEndpointChatCompletions, []byte(`{"reasoning_effort":"none","messages":[{"role":"user","content":"x"}]}`), "")
	require.True(t, ok)
	require.False(t, noReasoning.Reasoning)
}

func TestExtractCompositeRouteFeaturesResponses(t *testing.T) {
	body := `{"model":"auto","instructions":"system rules","reasoning":{"effort":"medium","summary":"auto"},"tools":[{"type":"function","name":"shell"}],
	"input":[
		{"type":"message","role":"user","content":[{"type":"input_text","text":"请翻译"},{"type":"input_image","image_url":"data:image/png;base64,AAAA"}]},
		{"type":"function_call","call_id":"c1","name":"shell","arguments":"{}"},
		{"type":"function_call_output","call_id":"c1","output":"done"}
	]}`

	features, ok := ExtractCompositeRouteFeatures(CompositeRouteEndpointResponses, []byte(body), "codex_cli_rs/0.40.0 (Mac OS 15.0; arm64)")

	require.True(t, ok)
	require.True(t, features.HasImages)
	require.True(t, features.HasTools)
	require.True(t, features.Reasoning)
	require.Equal(t, "请翻译", features.LastUserText)
	require.Equal(t, domain.CompositeRouteClientCodex, features.Client)

	plain, ok := ExtractCompositeRouteFeatures(CompositeRouteEndpointResponses, []byte(`{"input":"just text"}`), "GeminiCLI/0.1.5 (linux; x64)")
	require.True(t, ok)
	require.False(t, plain.Reasoning)
	require.False(t, plain.HasImages)
	require.Equal(t, "just text", plain.LastUserText)
	require.Equal(t, domain.CompositeRouteClientGeminiCLI, plain.Client)
}

func TestExtractCompositeRouteFeaturesIgnoresBase64InTokenEstimate(t *testing.T) {
	payload := strings.Repeat("A", 400000)
	body := `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"` + payload + `"}},{"type":"text","text":"hi"}]}]}`

	features, ok := ExtractCompositeRouteFeatures(CompositeRouteEndpointMessages, []byte(body), "")

	require.True(t, ok)
	require.Less(t, features.InputTokens, 100)
}

func TestExtractCompositeRouteFeaturesTruncatesLastUserTextPreview(t *testing.T) {
	text := strings.Repeat("字", 300)
	features, ok := ExtractCompositeRouteFeatures(CompositeRouteEndpointMessages, []byte(`{"messages":[{"role":"user","content":"`+text+`"}]}`), "")
	require.True(t, ok)
	require.Equal(t, strings.Repeat("字", compositeRouteLastUserTextPreviewRunes), features.LastUserText)
	require.Equal(t, text, features.lastUserText)
}

func TestExtractCompositeRouteFeaturesRejectsNonObjectBody(t *testing.T) {
	_, ok := ExtractCompositeRouteFeatures(CompositeRouteEndpointMessages, []byte(`[1,2]`), "")
	require.False(t, ok)
	_, ok = ExtractCompositeRouteFeatures(CompositeRouteEndpointEmbeddings, []byte(`{"input":"x"}`), "")
	require.False(t, ok)
}

func TestCompositeRouteConditionsTokenBoundsAndClients(t *testing.T) {
	features := CompositeRouteFeatures{InputTokens: 500, Client: domain.CompositeRouteClientCodex}
	require.Empty(t, compositeRouteConditionsMismatch(CompositeRouteConditions{MinInputTokens: 100, MaxInputTokens: 500}, features))
	require.NotEmpty(t, compositeRouteConditionsMismatch(CompositeRouteConditions{MinInputTokens: 501}, features))
	require.NotEmpty(t, compositeRouteConditionsMismatch(CompositeRouteConditions{MaxInputTokens: 499}, features))
	require.Empty(t, compositeRouteConditionsMismatch(CompositeRouteConditions{Clients: []string{"claude_code", "codex"}}, features))
	require.NotEmpty(t, compositeRouteConditionsMismatch(CompositeRouteConditions{Clients: []string{"claude_code"}}, features))
	features.lastUserText = "Please TRANSLATE this"
	require.Empty(t, compositeRouteConditionsMismatch(CompositeRouteConditions{Keywords: []string{"translate"}}, features))
}

func TestNormalizeCompositeRouteRulesValidation(t *testing.T) {
	valid := CompositeRouteRule{Enabled: true, TargetPlatform: PlatformGemini, UpstreamModel: "gemini-2.5-pro"}
	tooMany := make([]CompositeRouteRule, compositeRouteMaxRules+1)
	for i := range tooMany {
		tooMany[i] = valid
	}
	manyKeywords := make([]string, compositeRouteRuleMaxKeywords+1)
	for i := range manyKeywords {
		manyKeywords[i] = "k" + string(rune('a'+i))
	}

	for _, scenario := range []struct {
		name     string
		rules    []CompositeRouteRule
		endpoint string
		contains string
	}{
		{"too many rules", tooMany, CompositeRouteEndpointAny, "at most 20 rules"},
		{"endpoint without body", []CompositeRouteRule{valid}, CompositeRouteEndpointEmbeddings, "rules require route endpoint"},
		{"count_tokens endpoint", []CompositeRouteRule{valid}, CompositeRouteEndpointCountTokens, "rules require route endpoint"},
		{"non concrete platform", []CompositeRouteRule{{TargetPlatform: PlatformComposite, UpstreamModel: "x"}}, CompositeRouteEndpointAny, "rules[0]: target_platform"},
		{"missing upstream", []CompositeRouteRule{{TargetPlatform: PlatformOpenAI, UpstreamModel: "  "}}, CompositeRouteEndpointAny, "upstream_model is required"},
		{"long upstream", []CompositeRouteRule{{TargetPlatform: PlatformOpenAI, UpstreamModel: strings.Repeat("m", 201)}}, CompositeRouteEndpointAny, "upstream_model must be at most"},
		{"long name", []CompositeRouteRule{{Name: strings.Repeat("n", 65), TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5"}}, CompositeRouteEndpointAny, "name must be at most"},
		{"negative min", []CompositeRouteRule{{TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Conditions: CompositeRouteConditions{MinInputTokens: -1}}}, CompositeRouteEndpointAny, "min_input_tokens must be >= 0"},
		{"min above max", []CompositeRouteRule{{TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Conditions: CompositeRouteConditions{MinInputTokens: 10, MaxInputTokens: 5}}}, CompositeRouteEndpointAny, "min_input_tokens must be <= max_input_tokens"},
		{"unknown client", []CompositeRouteRule{{TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Conditions: CompositeRouteConditions{Clients: []string{"cursor"}}}}, CompositeRouteEndpointAny, "unsupported client"},
		{"blank keyword", []CompositeRouteRule{{TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Conditions: CompositeRouteConditions{Keywords: []string{" "}}}}, CompositeRouteEndpointAny, "keywords entries must be 1-100"},
		{"long keyword", []CompositeRouteRule{{TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Conditions: CompositeRouteConditions{Keywords: []string{strings.Repeat("k", 101)}}}}, CompositeRouteEndpointAny, "keywords entries must be 1-100"},
		{"too many keywords", []CompositeRouteRule{{TargetPlatform: PlatformOpenAI, UpstreamModel: "gpt-5", Conditions: CompositeRouteConditions{Keywords: manyKeywords}}}, CompositeRouteEndpointAny, "at most 20 entries"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			_, err := normalizeCompositeRouteRules(scenario.rules, scenario.endpoint)
			require.Error(t, err)
			require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
			require.Contains(t, err.Error(), scenario.contains)
		})
	}
}

func TestNormalizeCompositeRouteRulesTrimsAndDeduplicates(t *testing.T) {
	rules, err := normalizeCompositeRouteRules([]CompositeRouteRule{{
		Name:           " vision ",
		Enabled:        true,
		TargetPlatform: " gemini ",
		UpstreamModel:  " gemini-2.5-pro ",
		Conditions: CompositeRouteConditions{
			Clients:  []string{"Claude_Code", "claude_code", "codex"},
			Keywords: []string{" 翻译 ", "Translate", "translate", "翻译"},
		},
	}}, CompositeRouteEndpointMessages)

	require.NoError(t, err)
	require.Len(t, rules, 1)
	require.Equal(t, "vision", rules[0].Name)
	require.Equal(t, PlatformGemini, rules[0].TargetPlatform)
	require.Equal(t, "gemini-2.5-pro", rules[0].UpstreamModel)
	require.Equal(t, []string{"claude_code", "codex"}, rules[0].Conditions.Clients)
	require.Equal(t, []string{"翻译", "Translate"}, rules[0].Conditions.Keywords)

	empty, err := normalizeCompositeRouteRules(nil, CompositeRouteEndpointGemini)
	require.NoError(t, err)
	require.NotNil(t, empty)
	require.Empty(t, empty)
}

func TestCompositeModelMappingChainPrefixesPublicModel(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.RequestedPublicModel, "auto")
	ctx = context.WithValue(ctx, ctxkey.ResolvedUpstreamModel, "claude-sonnet-4-6")

	require.Equal(t, "auto→claude-sonnet-4-6", compositeModelMappingChain(ctx, ""))
	require.Equal(t, "auto→claude-sonnet-4-6", compositeModelMappingChain(ctx, "auto→claude-sonnet-4-6"))
	require.Equal(t, "auto→claude-sonnet-4-6→claude-sonnet-4-6-20260101", compositeModelMappingChain(ctx, "auto→claude-sonnet-4-6-20260101"))
	require.Equal(t, "auto→claude-sonnet-4-6→mapped", compositeModelMappingChain(ctx, "claude-sonnet-4-6→mapped"))

	same := context.WithValue(context.Background(), ctxkey.RequestedPublicModel, "gpt-5")
	same = context.WithValue(same, ctxkey.ResolvedUpstreamModel, "gpt-5")
	require.Equal(t, "gpt-5→gpt-5-mini", compositeModelMappingChain(same, "gpt-5→gpt-5-mini"))
	require.Equal(t, "a→b", compositeModelMappingChain(context.Background(), "a→b"))
}
