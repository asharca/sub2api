package service

import (
	"strings"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// compositeRouteLastUserTextPreviewRunes bounds the user text echoed back in
// route previews.
const compositeRouteLastUserTextPreviewRunes = 200

// CompositeRouteFeatures summarizes the request content smart rules match on.
type CompositeRouteFeatures struct {
	HasImages   bool   `json:"has_images"`
	HasTools    bool   `json:"has_tools"`
	Reasoning   bool   `json:"reasoning"`
	InputTokens int    `json:"input_tokens"`
	Client      string `json:"client"`
	// LastUserText is a preview of the most recent user message with text.
	LastUserText string `json:"last_user_text"`

	// lastUserText is the full text keywords are matched against.
	lastUserText string
}

// CompositeRouteFeatureEndpoint returns the body format smart rules read for a
// route endpoint, or "" when that endpoint carries no inspectable text body.
// count_tokens shares the messages format; any is read as messages.
func CompositeRouteFeatureEndpoint(endpoint string) string {
	switch normalizeCompositeRouteEndpoint(endpoint) {
	case CompositeRouteEndpointAny, CompositeRouteEndpointMessages, CompositeRouteEndpointCountTokens:
		return CompositeRouteEndpointMessages
	case CompositeRouteEndpointChatCompletions:
		return CompositeRouteEndpointChatCompletions
	case CompositeRouteEndpointResponses:
		return CompositeRouteEndpointResponses
	default:
		return ""
	}
}

// CompositeRouteSessionID returns the client conversation identifier that keeps
// a session on its smart-routing choice: Anthropic metadata.user_id sessions
// and the OpenAI session headers / prompt_cache_key.
func CompositeRouteSessionID(c *gin.Context, featureEndpoint string, body []byte) string {
	switch featureEndpoint {
	case CompositeRouteEndpointMessages:
		if parsed := ParseMetadataUserID(gjson.GetBytes(body, "metadata.user_id").String()); parsed != nil {
			return strings.TrimSpace(parsed.SessionID)
		}
		return ""
	case CompositeRouteEndpointChatCompletions, CompositeRouteEndpointResponses:
		return explicitOpenAIRequestSessionID(c, body)
	default:
		return ""
	}
}

// DetectCompositeRouteClient classifies the calling client from its User-Agent.
func DetectCompositeRouteClient(userAgent string) string {
	userAgent = strings.TrimSpace(userAgent)
	switch {
	case userAgent == "":
		return domain.CompositeRouteClientOther
	case claudeCliUserAgentRe.MatchString(userAgent):
		return domain.CompositeRouteClientClaudeCode
	case openai.IsCodexOfficialClientRequest(userAgent):
		return domain.CompositeRouteClientCodex
	case strings.HasPrefix(strings.ToLower(userAgent), "geminicli/"):
		return domain.CompositeRouteClientGeminiCLI
	default:
		return domain.CompositeRouteClientOther
	}
}

// ExtractCompositeRouteFeatures reads the smart-routing features from a
// request body in the given feature endpoint format (see
// CompositeRouteFeatureEndpoint). It returns false when the body is not a JSON
// object or the format carries no features.
func ExtractCompositeRouteFeatures(featureEndpoint string, body []byte, userAgent string) (CompositeRouteFeatures, bool) {
	root := parseRawJSONView(body)
	if !root.IsObject() {
		return CompositeRouteFeatures{}, false
	}
	features := CompositeRouteFeatures{Client: DetectCompositeRouteClient(userAgent)}
	var walker compositeFeatureWalker
	switch featureEndpoint {
	case CompositeRouteEndpointMessages:
		walker.walk(root.Get("system"), false)
		messages := root.Get("messages")
		walker.walk(messages, true)
		walker.walk(root.Get("tools"), false)
		features.HasTools = gjsonNonEmptyArray(root.Get("tools"))
		thinking := strings.ToLower(strings.TrimSpace(root.Get("thinking.type").String()))
		features.Reasoning = thinking != "" && thinking != "disabled"
		features.lastUserText = lastCompositeUserText(messages, false)
	case CompositeRouteEndpointChatCompletions:
		messages := root.Get("messages")
		walker.walk(messages, true)
		walker.walk(root.Get("tools"), false)
		walker.walk(root.Get("functions"), false)
		features.HasTools = gjsonNonEmptyArray(root.Get("tools")) || gjsonNonEmptyArray(root.Get("functions"))
		features.Reasoning = chatCompletionsReasoningEnabled(root)
		features.lastUserText = lastCompositeUserText(messages, false)
	case CompositeRouteEndpointResponses:
		walker.walk(root.Get("instructions"), false)
		input := root.Get("input")
		walker.walk(input, true)
		walker.walk(root.Get("tools"), false)
		features.HasTools = gjsonNonEmptyArray(root.Get("tools"))
		if reasoning := root.Get("reasoning"); reasoning.IsObject() {
			features.Reasoning = !strings.EqualFold(strings.TrimSpace(reasoning.Get("effort").String()), "none")
		}
		if input.Type == gjson.String {
			features.lastUserText = strings.TrimSpace(input.Str)
		} else {
			features.lastUserText = lastCompositeUserText(input, true)
		}
	default:
		return CompositeRouteFeatures{}, false
	}
	features.HasImages = walker.images
	features.InputTokens = walker.tokens
	features.LastUserText = strings.Clone(truncateRunes(features.lastUserText, compositeRouteLastUserTextPreviewRunes))
	return features, true
}

func chatCompletionsReasoningEnabled(root gjson.Result) bool {
	if effort := root.Get("reasoning_effort"); effort.Exists() {
		value := strings.ToLower(strings.TrimSpace(effort.String()))
		if value != "" {
			return value != "none"
		}
	}
	if reasoning := root.Get("reasoning"); reasoning.IsObject() {
		return !strings.EqualFold(strings.TrimSpace(reasoning.Get("effort").String()), "none")
	}
	if thinking := strings.ToLower(strings.TrimSpace(root.Get("thinking.type").String())); thinking != "" {
		return thinking != "disabled"
	}
	return root.Get("enable_thinking").Bool()
}

// lastCompositeUserText returns the text of the most recent user message that
// has any. responsesItems accepts Responses input items, which may omit type
// or use type "message".
func lastCompositeUserText(messages gjson.Result, responsesItems bool) string {
	if !messages.IsArray() {
		return ""
	}
	items := messages.Array()
	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		if item.Get("role").String() != "user" {
			continue
		}
		if responsesItems {
			if itemType := item.Get("type").String(); itemType != "" && itemType != "message" {
				continue
			}
		}
		if text := compositeUserContentText(item.Get("content")); text != "" {
			return text
		}
	}
	return ""
}

func compositeUserContentText(content gjson.Result) string {
	if content.Type == gjson.String {
		return strings.TrimSpace(content.Str)
	}
	if !content.IsArray() {
		return ""
	}
	var parts []string
	content.ForEach(func(_, part gjson.Result) bool {
		switch part.Get("type").String() {
		case "text", "input_text":
			if text := strings.TrimSpace(part.Get("text").String()); text != "" {
				parts = append(parts, text)
			}
		}
		return true
	})
	return strings.Join(parts, "\n")
}

func gjsonNonEmptyArray(value gjson.Result) bool {
	if !value.IsArray() {
		return false
	}
	nonEmpty := false
	value.ForEach(func(_, _ gjson.Result) bool {
		nonEmpty = true
		return false
	})
	return nonEmpty
}

func truncateRunes(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	count := 0
	for i := range s {
		if count == limit {
			return s[:i]
		}
		count++
	}
	return s
}

// compositeFeatureWalker estimates text tokens across a JSON subtree and
// notices image content parts. Binary payloads, signatures and identifiers are
// skipped so base64 data never inflates the estimate.
type compositeFeatureWalker struct {
	tokens int
	images bool
}

func (w *compositeFeatureWalker) walk(value gjson.Result, detectImages bool) {
	switch {
	case value.Type == gjson.String:
		w.tokens += estimateTokensForText(value.Str)
	case value.IsObject():
		if detectImages {
			switch value.Get("type").String() {
			case "image", "image_url", "input_image":
				w.images = true
			}
		}
		value.ForEach(func(key, child gjson.Result) bool {
			switch key.Str {
			case "type", "id", "tool_use_id", "call_id", "cache_control", "media_type",
				"data", "source", "image_url", "file_data", "file_id", "signature", "encrypted_content":
				return true
			}
			w.walk(child, detectImages)
			return true
		})
	case value.IsArray():
		value.ForEach(func(_, child gjson.Result) bool {
			w.walk(child, detectImages)
			return true
		})
	}
}
