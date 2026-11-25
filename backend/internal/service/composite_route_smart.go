package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"
)

const (
	// compositeRouteStickyTTL is how long a session keeps its smart-rule choice
	// after its last request.
	compositeRouteStickyTTL = time.Hour

	compositeRouteTraceReasonDisabled       = "rule disabled"
	compositeRouteTraceReasonUnavailable    = "no schedulable account"
	compositeRouteTraceReasonNotChecked     = "not checked: an earlier candidate was selected"
	compositeRouteTraceReasonSticky         = "sticky session"
	compositeRouteTraceReasonAllUnavailable = "no candidate has a schedulable account; using the first candidate"
)

// CompositeRouteTraceItem records how one smart-rule candidate was evaluated.
// RuleIndex -1 is the route's own target.
type CompositeRouteTraceItem struct {
	RuleIndex      int    `json:"rule_index"`
	RuleName       string `json:"rule_name,omitempty"`
	TargetPlatform string `json:"target_platform"`
	UpstreamModel  string `json:"upstream_model"`
	Matched        bool   `json:"matched"`
	Available      bool   `json:"available"`
	Selected       bool   `json:"selected"`
	Reason         string `json:"reason,omitempty"`
}

// CompositeRouteRequestContent is the request body context smart rules are
// evaluated against. Entry points that cannot read a text body pass none and
// get the route's own target.
type CompositeRouteRequestContent struct {
	// FeatureEndpoint is the body format: messages, chat_completions or
	// responses (see CompositeRouteFeatureEndpoint).
	FeatureEndpoint string
	Body            []byte
	UserAgent       string
	// SessionID lazily returns the client conversation ID that enables
	// session affinity; nil or "" means no affinity.
	SessionID func() string
	// StickyReadOnly reuses an existing session choice without writing one
	// (count_tokens).
	StickyReadOnly bool
	// Preview checks every matched candidate's availability for the trace and
	// never touches session affinity.
	Preview bool
}

// CompositeRouteStickyStore persists the per-session smart-rule choice of a
// composite route. Values are "target_platform|upstream_model".
type CompositeRouteStickyStore interface {
	GetCompositeRouteSticky(ctx context.Context, groupID, routeID int64, sessionHash string) (string, bool, error)
	SetCompositeRouteSticky(ctx context.Context, groupID, routeID int64, sessionHash, target string, ttl time.Duration) error
}

// CompositeTargetAvailabilityChecker reports whether the group currently has a
// schedulable account on platform that supports model.
type CompositeTargetAvailabilityChecker func(ctx context.Context, groupID int64, platform, model string) (bool, error)

func (r *CompositeRouteResolver) SetTargetAvailabilityChecker(checker CompositeTargetAvailabilityChecker) {
	if r != nil {
		r.availabilityChecker = checker
	}
}

func (r *CompositeRouteResolver) SetStickyStore(store CompositeRouteStickyStore) {
	if r != nil {
		r.stickyStore = store
	}
}

func compositeRouteStickySessionHash(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return hex.EncodeToString(sum[:16])
}

func compositeRouteStickyValue(platform, model string) string {
	return platform + "|" + model
}

// compositeRouteSmartEval carries one smart-rule evaluation.
type compositeRouteSmartEval struct {
	resolver *CompositeRouteResolver
	groupID  int64
	route    *CompositeModelRoute
	features CompositeRouteFeatures
	trace    []CompositeRouteTraceItem
	// conditions[i] are trace[i]'s rule conditions; nil for the route target.
	conditions []*CompositeRouteConditions
	// availability caches per-trace-entry checks: 0 unknown, 1 yes, 2 no.
	availability []uint8
}

func (e *compositeRouteSmartEval) available(ctx context.Context, pos int) bool {
	switch e.availability[pos] {
	case 1:
		return true
	case 2:
		return false
	}
	ok := true
	if checker := e.resolver.availabilityChecker; checker != nil {
		item := e.trace[pos]
		available, err := checker(ctx, e.groupID, item.TargetPlatform, item.UpstreamModel)
		if err != nil {
			// A failed check must not take a target out of rotation; scheduling
			// reports the real error if the target cannot serve.
			slog.Warn("composite_smart_route_availability_check_failed",
				"group_id", e.groupID,
				"route_id", e.route.ID,
				"target_platform", item.TargetPlatform,
				"upstream_model", item.UpstreamModel,
				"error", err)
		} else {
			ok = available
		}
	}
	e.availability[pos] = 2
	if ok {
		e.availability[pos] = 1
	}
	e.trace[pos].Available = ok
	return ok
}

// applyCompositeSmartRules picks the decision's target from the matched route's
// rules and the request content, honoring availability and session affinity.
func (r *CompositeRouteResolver) applyCompositeSmartRules(ctx context.Context, decision *CompositeRouteDecision, route *CompositeModelRoute, content *CompositeRouteRequestContent) {
	features, ok := ExtractCompositeRouteFeatures(content.FeatureEndpoint, content.Body, content.UserAgent)
	if !ok {
		return
	}
	eval := &compositeRouteSmartEval{
		resolver:     r,
		groupID:      decision.GroupID,
		route:        route,
		features:     features,
		trace:        make([]CompositeRouteTraceItem, 0, len(route.Rules)+1),
		conditions:   make([]*CompositeRouteConditions, 0, len(route.Rules)+1),
		availability: make([]uint8, len(route.Rules)+1),
	}
	for i := range route.Rules {
		rule := &route.Rules[i]
		item := CompositeRouteTraceItem{
			RuleIndex:      i,
			RuleName:       rule.Name,
			TargetPlatform: rule.TargetPlatform,
			UpstreamModel:  rule.UpstreamModel,
		}
		if !rule.Enabled {
			item.Reason = compositeRouteTraceReasonDisabled
		} else if reason := compositeRouteConditionsMismatch(rule.Conditions, features); reason != "" {
			item.Reason = reason
		} else {
			item.Matched = true
		}
		eval.trace = append(eval.trace, item)
		eval.conditions = append(eval.conditions, &rule.Conditions)
	}
	eval.trace = append(eval.trace, CompositeRouteTraceItem{
		RuleIndex:      -1,
		TargetPlatform: decision.TargetPlatform,
		UpstreamModel:  decision.UpstreamModel,
		Matched:        true,
	})
	eval.conditions = append(eval.conditions, nil)

	selected := -1
	firstMatched := -1
	for pos := range eval.trace {
		if !eval.trace[pos].Matched {
			continue
		}
		if firstMatched < 0 {
			firstMatched = pos
		}
		if selected >= 0 {
			if content.Preview {
				if !eval.available(ctx, pos) {
					eval.trace[pos].Reason = compositeRouteTraceReasonUnavailable
				}
			} else {
				eval.trace[pos].Reason = compositeRouteTraceReasonNotChecked
			}
			continue
		}
		if eval.available(ctx, pos) {
			selected = pos
		} else {
			eval.trace[pos].Reason = compositeRouteTraceReasonUnavailable
		}
	}
	if selected < 0 {
		// The route's own target always matches, so firstMatched exists.
		selected = firstMatched
		eval.trace[selected].Reason = compositeRouteTraceReasonAllUnavailable
	}

	sticky := false
	sessionID := ""
	if !content.Preview && route.SessionAffinity && content.SessionID != nil && r.stickyStore != nil && route.ID > 0 {
		sessionID = strings.TrimSpace(content.SessionID())
	}
	if sessionID != "" {
		sessionHash := compositeRouteStickySessionHash(sessionID)
		if pos, ok := eval.stickyPosition(ctx, sessionHash, selected); ok {
			selected = pos
			sticky = true
			eval.trace[pos].Reason = compositeRouteTraceReasonSticky
		}
		if !content.StickyReadOnly {
			item := eval.trace[selected]
			if err := r.stickyStore.SetCompositeRouteSticky(ctx, decision.GroupID, route.ID, sessionHash, compositeRouteStickyValue(item.TargetPlatform, item.UpstreamModel), compositeRouteStickyTTL); err != nil {
				slog.Warn("composite_smart_route_sticky_write_failed", "group_id", decision.GroupID, "route_id", route.ID, "error", err)
			}
		}
	}

	eval.trace[selected].Selected = true
	chosen := eval.trace[selected]
	ruleIndex := chosen.RuleIndex
	decision.TargetPlatform = chosen.TargetPlatform
	decision.UpstreamModel = chosen.UpstreamModel
	decision.RuleIndex = &ruleIndex
	decision.RuleName = chosen.RuleName
	decision.Sticky = sticky
	decision.Features = &eval.features
	decision.Trace = eval.trace

	if !content.Preview {
		slog.Info("composite_smart_route_selected",
			"group_id", decision.GroupID,
			"route_id", route.ID,
			"public_model", decision.PublicModel,
			"endpoint", decision.Endpoint,
			"rule_index", ruleIndex,
			"rule_name", chosen.RuleName,
			"target_platform", chosen.TargetPlatform,
			"upstream_model", chosen.UpstreamModel,
			"sticky", sticky,
			"has_images", features.HasImages,
			"has_tools", features.HasTools,
			"reasoning", features.Reasoning,
			"input_tokens", features.InputTokens,
			"client", features.Client,
		)
	}
}

// stickyPosition returns the trace entry of the session's earlier choice when
// it may still be used: it is still the route target or an enabled rule's
// target, its capability conditions still hold, it is available, and the fresh
// choice at freshPos does not demand a capability the sticky rule lacks.
func (e *compositeRouteSmartEval) stickyPosition(ctx context.Context, sessionHash string, freshPos int) (int, bool) {
	value, found, err := e.resolver.stickyStore.GetCompositeRouteSticky(ctx, e.groupID, e.route.ID, sessionHash)
	if err != nil {
		slog.Warn("composite_smart_route_sticky_read_failed", "group_id", e.groupID, "route_id", e.route.ID, "error", err)
		return -1, false
	}
	if !found {
		return -1, false
	}
	platform, model, ok := strings.Cut(value, "|")
	if !ok {
		return -1, false
	}
	// The fresh choice already serves the sticky target: nothing switches.
	if fresh := e.trace[freshPos]; fresh.TargetPlatform == platform && fresh.UpstreamModel == model {
		return freshPos, true
	}
	for pos, item := range e.trace {
		if item.TargetPlatform != platform || item.UpstreamModel != model {
			continue
		}
		if item.RuleIndex >= 0 && !e.route.Rules[item.RuleIndex].Enabled {
			continue
		}
		conditions := e.conditions[pos]
		if conditions != nil && compositeRouteCapabilityMismatch(*conditions, e.features) != "" {
			continue
		}
		if pos != freshPos && compositeRouteCapabilityUpgrade(e.conditions[freshPos], conditions) {
			return -1, false
		}
		if !e.available(ctx, pos) {
			return -1, false
		}
		return pos, true
	}
	return -1, false
}
