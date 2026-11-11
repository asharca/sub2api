package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *OpenAIGatewayHandler) cyberPolicyLogOnly(c *gin.Context, apiKey *service.APIKey) bool {
	return h != nil && c != nil && c.Request != nil && h.gatewayService.CyberPolicyLogOnly(c.Request.Context(), apiKey)
}

// Both HTTP and WebSocket admission skip existing blocks for trusted users.
// A verified or inherited identity takes precedence over transcript fallback.
func (h *OpenAIGatewayHandler) findBlockedCyberSession(c *gin.Context, apiKey *service.APIKey, identity service.CyberSessionIdentityResolution, body []byte) string {
	if apiKey == nil || h.cyberPolicyLogOnly(c, apiKey) {
		return ""
	}
	if identity.Resolved() {
		return h.gatewayService.FindCyberSessionBlockedForIdentity(c.Request.Context(), identity)
	}
	return findBlockedCyberSessionKey(c.Request.Context(), h.gatewayService, apiKey.ID, c, body)
}
