package domain

// Composite smart-routing client identifiers usable in rule conditions.
const (
	CompositeRouteClientClaudeCode = "claude_code"
	CompositeRouteClientCodex      = "codex"
	CompositeRouteClientGeminiCLI  = "gemini_cli"
	CompositeRouteClientOther      = "other"
)

// CompositeRouteRule is one ordered smart rule attached to an explicit
// composite route. When every set condition matches the request, the rule's
// concrete platform/upstream model is a routing candidate ahead of the route's
// own target.
type CompositeRouteRule struct {
	Name           string                   `json:"name"`
	Enabled        bool                     `json:"enabled"`
	TargetPlatform string                   `json:"target_platform"`
	UpstreamModel  string                   `json:"upstream_model"`
	Conditions     CompositeRouteConditions `json:"conditions"`
}

// CompositeRouteConditions are ANDed request-feature checks. Nil booleans,
// zero token bounds and empty lists do not restrict the match.
type CompositeRouteConditions struct {
	HasImages      *bool    `json:"has_images,omitempty"`
	HasTools       *bool    `json:"has_tools,omitempty"`
	Reasoning      *bool    `json:"reasoning,omitempty"`
	MinInputTokens int      `json:"min_input_tokens"`
	MaxInputTokens int      `json:"max_input_tokens"`
	Clients        []string `json:"clients,omitempty"`
	Keywords       []string `json:"keywords,omitempty"`
}
