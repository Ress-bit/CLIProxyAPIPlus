package management

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	kiroauth "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/kiro"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

// Quota exceeded toggles
func (h *Handler) GetSwitchProject(c *gin.Context) {
	c.JSON(200, gin.H{"switch-project": h.cfg.QuotaExceeded.SwitchProject})
}
func (h *Handler) PutSwitchProject(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.QuotaExceeded.SwitchProject = v })
}

func (h *Handler) GetSwitchPreviewModel(c *gin.Context) {
	c.JSON(200, gin.H{"switch-preview-model": h.cfg.QuotaExceeded.SwitchPreviewModel})
}
func (h *Handler) PutSwitchPreviewModel(c *gin.Context) {
	h.updateBoolField(c, func(v bool) { h.cfg.QuotaExceeded.SwitchPreviewModel = v })
}

// KiroQuotaResponse represents the quota information for Kiro (AWS CodeWhisperer)
type KiroQuotaResponse struct {
	TotalLimit       float64               `json:"total_limit"`
	CurrentUsage     float64               `json:"current_usage"`
	RemainingQuota   float64               `json:"remaining_quota"`
	IsExhausted      bool                  `json:"is_exhausted"`
	ResourceType     string                `json:"resource_type"`
	NextReset        *time.Time            `json:"next_reset,omitempty"`
	UsagePercentage  float64               `json:"usage_percentage"`
	SubscriptionInfo *KiroSubscriptionInfo `json:"subscription_info,omitempty"`
	Breakdown        []KiroQuotaBreakdown  `json:"breakdown,omitempty"`
}

// KiroSubscriptionInfo contains subscription details
type KiroSubscriptionInfo struct {
	Title string `json:"title"`
	Type  string `json:"type"`
}

// KiroQuotaBreakdown contains detailed usage breakdown
type KiroQuotaBreakdown struct {
	ResourceType string  `json:"resource_type"`
	Limit        float64 `json:"limit"`
	CurrentUsage float64 `json:"current_usage"`
	Remaining    float64 `json:"remaining"`
	IsFreeTrial  bool    `json:"is_free_trial"`
}

// GetKiroQuota fetches Kiro (AWS CodeWhisperer) quota information from the /getUsageLimits endpoint.
//
// Endpoint:
//
//	GET /v0/management/kiro-quota
//
// Query Parameters (optional):
//   - auth_index: The credential "auth_index" from GET /v0/management/auth-files.
//     If omitted, uses the first available Kiro credential.
//
// Response:
//
//	Returns the KiroQuotaResponse with quota information including total_limit,
//	current_usage, remaining_quota, and usage_percentage.
//
// Example:
//
//	curl -sS -X GET "http://127.0.0.1:8317/v0/management/kiro-quota?auth_index=<AUTH_INDEX>" \
//	  -H "Authorization: Bearer <MANAGEMENT_KEY>"
func (h *Handler) GetKiroQuota(c *gin.Context) {
	authIndex := strings.TrimSpace(c.Query("auth_index"))
	if authIndex == "" {
		authIndex = strings.TrimSpace(c.Query("authIndex"))
	}
	if authIndex == "" {
		authIndex = strings.TrimSpace(c.Query("AuthIndex"))
	}

	auth := h.findKiroAuth(authIndex)
	if auth == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no kiro credential found"})
		return
	}

	// Extract token data from auth metadata
	accessToken, _ := auth.Metadata["access_token"].(string)
	profileArn, _ := auth.Metadata["profile_arn"].(string)
	refreshToken, _ := auth.Metadata["refresh_token"].(string)

	if accessToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kiro access token not found"})
		return
	}

	// Create usage checker
	usageChecker := kiroauth.NewUsageChecker(h.cfg)

	// Build token data
	tokenData := &kiroauth.KiroTokenData{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ProfileArn:   profileArn,
	}

	// Fetch quota status
	quotaStatus, err := usageChecker.GetQuotaStatus(c.Request.Context(), tokenData)
	if err != nil {
		log.WithError(err).Debug("kiro quota request failed")
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to fetch kiro quota", "details": err.Error()})
		return
	}

	// Calculate usage percentage
	usagePercentage := 0.0
	if quotaStatus.TotalLimit > 0 {
		usagePercentage = (quotaStatus.CurrentUsage / quotaStatus.TotalLimit) * 100
	}

	// Build response
	response := KiroQuotaResponse{
		TotalLimit:      quotaStatus.TotalLimit,
		CurrentUsage:    quotaStatus.CurrentUsage,
		RemainingQuota:  quotaStatus.RemainingQuota,
		IsExhausted:     quotaStatus.IsExhausted,
		ResourceType:    quotaStatus.ResourceType,
		UsagePercentage: usagePercentage,
	}

	// Add next reset time if available
	if !quotaStatus.NextReset.IsZero() {
		response.NextReset = &quotaStatus.NextReset
	}

	c.JSON(http.StatusOK, response)
}

// findKiroAuth locates a Kiro credential by auth_index or returns the first available one
func (h *Handler) findKiroAuth(authIndex string) *coreauth.Auth {
	if h == nil || h.authManager == nil {
		return nil
	}

	auths := h.authManager.List()
	var firstKiro *coreauth.Auth

	for _, auth := range auths {
		if auth == nil {
			continue
		}

		provider := strings.ToLower(strings.TrimSpace(auth.Provider))
		if provider != "kiro" && provider != "codewhisperer" && provider != "aws" {
			continue
		}

		if firstKiro == nil {
			firstKiro = auth
		}

		if authIndex != "" {
			auth.EnsureIndex()
			if auth.Index == authIndex {
				return auth
			}
		}
	}

	return firstKiro
}
