package routes

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func smartRoutingTestResolver() *service.CompositeRouteResolver {
	return service.NewCompositeRouteResolver(compositeRouteRepoStub{
		routes: []service.CompositeModelRoute{{
			ID:              3,
			GroupID:         1,
			PublicModel:     "auto",
			MatchType:       service.CompositeRouteMatchExact,
			TargetPlatform:  service.PlatformAnthropic,
			UpstreamModel:   "claude-sonnet-4-6",
			Endpoint:        service.CompositeRouteEndpointAny,
			Priority:        100,
			Enabled:         true,
			SessionAffinity: true,
			Rules: []service.CompositeRouteRule{{
				Name:           "vision",
				Enabled:        true,
				TargetPlatform: service.PlatformGemini,
				UpstreamModel:  "gemini-2.5-pro",
				Conditions:     service.CompositeRouteConditions{HasImages: new(true)},
			}},
		}},
	})
}

func smartRoutingTestRouter(resolver *service.CompositeRouteResolver, path string, handler gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(gin.HandlerFunc(servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
		groupID := int64(1)
		c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
			GroupID: &groupID,
			Group:   &service.Group{ID: groupID, Platform: service.PlatformComposite},
		})
		c.Next()
	})))
	router.Use(compositeTargetPlatformMiddleware(resolver))
	router.POST(path, handler)
	return router
}

func TestCompositeTargetPlatformMiddlewareAppliesSmartRules(t *testing.T) {
	router := smartRoutingTestRouter(smartRoutingTestResolver(), "/v1/messages", func(c *gin.Context) {
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, service.PlatformGemini, platform)
		publicModel, ok := service.RequestedPublicModelFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, "auto", publicModel)

		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), `"model":"gemini-2.5-pro"`)
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestCompositeTargetPlatformMiddlewareSmartRouteDefaultsWithoutFeatures(t *testing.T) {
	router := smartRoutingTestRouter(smartRoutingTestResolver(), "/v1/chat/completions", func(c *gin.Context) {
		platform, ok := service.ResolvedTargetPlatformFromContext(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, service.PlatformAnthropic, platform)

		body, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"model":"claude-sonnet-4-6","messages":[{"role":"user","content":"hi"}]}`, string(body))
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"auto","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNoContent, w.Code)
}

func TestCompositeRouteRequestContentOnlyForTextEntryPoints(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"auto","metadata":{"user_id":"user_a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2_account__session_0b8c7e5c-4f0e-4a6c-9b38-2d5a6b0b1e11"}}`)
	for _, scenario := range []struct {
		path            string
		featureEndpoint string
		readOnly        bool
	}{
		{"/v1/messages", service.CompositeRouteEndpointMessages, false},
		{"/v1/messages/count_tokens", service.CompositeRouteEndpointMessages, true},
		{"/v1/chat/completions", service.CompositeRouteEndpointChatCompletions, false},
		{"/v1/responses", service.CompositeRouteEndpointResponses, false},
		{"/backend-api/codex/responses", service.CompositeRouteEndpointResponses, false},
		{"/v1/alpha/search", "", false},
		{"/v1/realtime/calls", "", false},
		{"/v1/embeddings", "", false},
		{"/v1/images/generations", "", false},
	} {
		t.Run(scenario.path, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, scenario.path, nil)
			c.Request.Header.Set("session_id", "sess-openai")
			content := compositeRouteRequestContent(c, compositeRouteEndpointForPath(scenario.path), body)
			if scenario.featureEndpoint == "" {
				require.Nil(t, content)
				return
			}
			require.NotNil(t, content)
			require.Equal(t, scenario.featureEndpoint, content.FeatureEndpoint)
			require.Equal(t, scenario.readOnly, content.StickyReadOnly)
			require.NotNil(t, content.SessionID)
			if scenario.featureEndpoint == service.CompositeRouteEndpointMessages {
				require.Equal(t, "0b8c7e5c-4f0e-4a6c-9b38-2d5a6b0b1e11", content.SessionID())
			} else {
				require.Equal(t, "sess-openai", content.SessionID())
			}
		})
	}
}
