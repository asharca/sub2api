//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestAdminService_CreateCompositeRoutePersistsRulesAndAffinity(t *testing.T) {
	groupRepo := &groupRepoStubForAdmin{getByID: &Group{ID: 7, Platform: PlatformComposite}}
	routeRepo := &compositeRouteRepoStubForAdmin{nextID: 5}
	svc := &adminServiceImpl{groupRepo: groupRepo, compositeRouteRepo: routeRepo}

	route, err := svc.CreateCompositeRoute(context.Background(), 7, CompositeRouteInput{
		PublicModel:     "auto",
		TargetPlatform:  PlatformAnthropic,
		UpstreamModel:   "claude-sonnet-4-6",
		Endpoint:        CompositeRouteEndpointAny,
		Enabled:         true,
		SessionAffinity: false,
		Rules:           []CompositeRouteRule{{Name: "vision", Enabled: true, TargetPlatform: PlatformGemini, UpstreamModel: "gemini-2.5-pro", Conditions: CompositeRouteConditions{HasImages: new(true)}}},
	})

	require.NoError(t, err)
	require.False(t, route.SessionAffinity)
	require.Len(t, routeRepo.created.Rules, 1)
	require.Equal(t, "vision", routeRepo.created.Rules[0].Name)
}

func TestAdminService_CreateCompositeRouteRejectsInvalidRules(t *testing.T) {
	groupRepo := &groupRepoStubForAdmin{getByID: &Group{ID: 7, Platform: PlatformComposite}}
	routeRepo := &compositeRouteRepoStubForAdmin{nextID: 5}
	svc := &adminServiceImpl{groupRepo: groupRepo, compositeRouteRepo: routeRepo}

	_, err := svc.CreateCompositeRoute(context.Background(), 7, CompositeRouteInput{
		PublicModel:    "auto",
		TargetPlatform: PlatformAnthropic,
		Endpoint:       CompositeRouteEndpointImages,
		Enabled:        true,
		Rules:          []CompositeRouteRule{{Enabled: true, TargetPlatform: PlatformGemini, UpstreamModel: "gemini-2.5-pro"}},
	})

	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
	require.Nil(t, routeRepo.created)
}

func TestAdminService_PreviewCompositeRouteEvaluatesSmartRules(t *testing.T) {
	groupRepo := &groupRepoStubForAdmin{getByID: &Group{ID: 7, Platform: PlatformComposite}}
	routeRepo := &compositeRouteRepoStubForAdmin{routes: []CompositeModelRoute{smartAutoRoute()}}
	svc := &adminServiceImpl{groupRepo: groupRepo, compositeRouteRepo: routeRepo}

	withBody, err := svc.PreviewCompositeRoute(context.Background(), 7, CompositeRoutePreviewRequest{
		Model:     "auto",
		Endpoint:  CompositeRouteEndpointAny,
		Body:      json.RawMessage(translateMessagesBody),
		UserAgent: "claude-cli/2.1.0",
	})
	require.NoError(t, err)
	require.Equal(t, PlatformDeepseek, withBody.TargetPlatform)
	require.Equal(t, "deepseek-chat", withBody.UpstreamModel)
	require.Equal(t, 2, *withBody.RuleIndex)
	require.Equal(t, "translate", withBody.RuleName)
	require.False(t, withBody.Sticky)
	require.Equal(t, domain.CompositeRouteClientClaudeCode, withBody.Features.Client)
	require.Len(t, withBody.Trace, 4)
	require.Equal(t, -1, withBody.Trace[3].RuleIndex)

	raw, err := json.Marshal(withBody)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"rule_index":2`)
	require.Contains(t, string(raw), `"features":{`)
	require.Contains(t, string(raw), `"trace":[`)

	noBody, err := svc.PreviewCompositeRoute(context.Background(), 7, CompositeRoutePreviewRequest{Model: "auto", Endpoint: CompositeRouteEndpointMessages})
	require.NoError(t, err)
	require.Equal(t, PlatformAnthropic, noBody.TargetPlatform)
	require.Nil(t, noBody.RuleIndex)
	raw, err = json.Marshal(noBody)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"rule_index"`)
	require.NotContains(t, string(raw), `"features"`)
	require.NotContains(t, string(raw), `"trace"`)

	_, err = svc.PreviewCompositeRoute(context.Background(), 7, CompositeRoutePreviewRequest{Model: "auto", Body: json.RawMessage(`[1]`)})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, infraerrors.Code(err))
}
