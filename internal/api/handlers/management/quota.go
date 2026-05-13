package management

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	kiroauth "github.com/router-for-me/CLIProxyAPI/v7/internal/auth/kiro"
	coreauth "github.com/router-for-me/CLIProxyAPI/v7/sdk/cliproxy/auth"
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
	AuthIndex        string                `json:"auth_index"`
	Email            string                `json:"email,omitempty"`
	ProfileArn       string                `json:"profile_arn,omitempty"`
	TotalLimit       float64               `json:"total_limit"`
	CurrentUsage     float64               `json:"current_usage"`
	RemainingQuota   float64               `json:"remaining_quota"`
	IsExhausted      bool                  `json:"is_exhausted"`
	ResourceType     string                `json:"resource_type"`
	NextReset        *time.Time            `json:"next_reset,omitempty"`
	UsagePercentage  float64               `json:"usage_percentage"`
	SubscriptionInfo *KiroSubscriptionInfo `json:"subscription_info,omitempty"`
	Breakdowns       []KiroQuotaBreakdown  `json:"breakdowns,omitempty"`
}

// KiroSubscriptionInfo contains subscription details
type KiroSubscriptionInfo struct {
	Title string `json:"title"`
	Type  string `json:"type"`
}

// KiroQuotaBreakdown contains detailed usage breakdown
type KiroQuotaBreakdown struct {
	ID           string     `json:"id"`
	Label        string     `json:"label"`
	ResourceType string     `json:"resource_type"`
	Limit        float64    `json:"limit"`
	CurrentUsage float64    `json:"current_usage"`
	Remaining    float64    `json:"remaining"`
	IsFreeTrial  bool       `json:"is_free_trial"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	NextReset    *time.Time `json:"next_reset,omitempty"`
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

	credits, creditsErr := usageChecker.CheckCredits(c.Request.Context(), tokenData)
	usage, usageErr := usageChecker.CheckUsage(c.Request.Context(), tokenData)
	if creditsErr != nil && usageErr != nil {
		log.WithError(creditsErr).Debug("kiro credits request failed")
		log.WithError(usageErr).Debug("kiro usage request failed")
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "failed to fetch kiro quota",
			"details": fmt.Sprintf("credits: %v; usage: %v", creditsErr, usageErr),
		})
		return
	}

	// Extract email from auth metadata
	email, _ := auth.Metadata["email"].(string)
	if email == "" {
		email = auth.Attributes["email"]
	}

	// Ensure auth index is set
	auth.EnsureIndex()

	// Build response
	response := KiroQuotaResponse{
		AuthIndex:  auth.Index,
		Email:      email,
		ProfileArn: profileArn,
	}

	if credits != nil {
		response.RemainingQuota = credits.RemainingCredits
		response.ResourceType = "CREDITS"
		if credits.HasTotalCredits {
			response.TotalLimit = credits.TotalCredits
			response.CurrentUsage = credits.TotalCredits - credits.RemainingCredits
			if response.CurrentUsage < 0 {
				response.CurrentUsage = 0
			}
			if response.TotalLimit > 0 {
				response.UsagePercentage = (response.CurrentUsage / response.TotalLimit) * 100
			}
			response.IsExhausted = credits.RemainingCredits <= 0
		}
		if credits.ResetDate != nil {
			response.NextReset = credits.ResetDate
		}
		if credits.SubscriptionType != "" {
			response.SubscriptionInfo = &KiroSubscriptionInfo{
				Title: credits.SubscriptionType,
				Type:  credits.SubscriptionType,
			}
		}
		response.Breakdowns = buildKiroCreditsBreakdowns(credits)
	}

	if usage != nil {
		if response.SubscriptionInfo == nil && usage.SubscriptionInfo != nil {
			response.SubscriptionInfo = &KiroSubscriptionInfo{
				Title: usage.SubscriptionInfo.SubscriptionTitle,
				Type:  usage.SubscriptionInfo.Type,
			}
		}
		if len(response.Breakdowns) == 0 {
			response.Breakdowns = buildKiroQuotaBreakdowns(usage)
		}
		if response.TotalLimit <= 0 {
			quotaStatus := kiroauth.BuildQuotaStatus(usage)
			response.TotalLimit = quotaStatus.TotalLimit
			response.CurrentUsage = quotaStatus.CurrentUsage
			response.RemainingQuota = quotaStatus.RemainingQuota
			response.IsExhausted = quotaStatus.IsExhausted
			response.ResourceType = quotaStatus.ResourceType
			if response.TotalLimit > 0 {
				response.UsagePercentage = (response.CurrentUsage / response.TotalLimit) * 100
			}
			if response.NextReset == nil && !quotaStatus.NextReset.IsZero() {
				response.NextReset = &quotaStatus.NextReset
			}
		}
	}

	if response.ResourceType == "" {
		response.ResourceType = "CREDITS"
	}

	c.JSON(http.StatusOK, response)
}

func buildKiroCreditsBreakdowns(credits *kiroauth.KiroCreditsResponse) []KiroQuotaBreakdown {
	if credits == nil {
		return nil
	}

	limit := 0.0
	current := 0.0
	if credits.HasTotalCredits {
		limit = credits.TotalCredits
		current = credits.TotalCredits - credits.RemainingCredits
		if current < 0 {
			current = 0
		}
	}

	return []KiroQuotaBreakdown{
		{
			ID:           "credits",
			Label:        "Credit",
			ResourceType: "CREDITS",
			Limit:        limit,
			CurrentUsage: current,
			Remaining:    credits.RemainingCredits,
			NextReset:    credits.ResetDate,
		},
	}
}

func buildKiroQuotaBreakdowns(usage *kiroauth.UsageQuotaResponse) []KiroQuotaBreakdown {
	if usage == nil {
		return nil
	}

	breakdowns := make([]KiroQuotaBreakdown, 0, len(usage.UsageBreakdownList)*2)
	for idx, entry := range usage.UsageBreakdownList {
		base := buildBreakdownEntry(entry, idx, "", false)
		if base.Limit > 0 || base.CurrentUsage > 0 {
			breakdowns = append(breakdowns, base)
		}

		if entry.FreeTrialInfo != nil {
			free := buildFreeTrialEntry(entry, idx)
			if free.Limit > 0 || free.CurrentUsage > 0 {
				breakdowns = append(breakdowns, free)
			}
		}
	}

	return breakdowns
}

func buildBreakdownEntry(entry kiroauth.UsageBreakdownExtended, idx int, suffix string, isFree bool) KiroQuotaBreakdown {
	label := strings.TrimSpace(entry.DisplayName)
	if label == "" {
		label = entry.ResourceType
	}
	if suffix != "" {
		label = fmt.Sprintf("%s (%s)", label, suffix)
	}

	limit := entry.UsageLimitWithPrecision
	current := entry.CurrentUsageWithPrecision
	remaining := limit - current
	if remaining < 0 {
		remaining = 0
	}

	return KiroQuotaBreakdown{
		ID:           fmt.Sprintf("%s_%d_%s", entry.ResourceType, idx, suffix),
		Label:        label,
		ResourceType: entry.ResourceType,
		Limit:        limit,
		CurrentUsage: current,
		Remaining:    remaining,
		IsFreeTrial:  isFree,
		NextReset:    kiroauth.ParseUnixTimestamp(entry.NextDateReset),
	}
}

func buildFreeTrialEntry(entry kiroauth.UsageBreakdownExtended, idx int) KiroQuotaBreakdown {
	freeLimit := entry.FreeTrialInfo.UsageLimitWithPrecision
	freeUsage := entry.FreeTrialInfo.CurrentUsageWithPrecision
	freeRemaining := freeLimit - freeUsage
	if freeRemaining < 0 {
		freeRemaining = 0
	}

	label := strings.TrimSpace(entry.DisplayName)
	if label == "" {
		label = entry.ResourceType
	}
	label = fmt.Sprintf("%s (Free Trial)", label)

	expiresAt := kiroauth.ParseUnixTimestamp(entry.FreeTrialInfo.FreeTrialExpiry)
	nextReset := kiroauth.ParseUnixTimestamp(entry.FreeTrialInfo.NextDateReset)
	if nextReset == nil {
		nextReset = kiroauth.ParseUnixTimestamp(entry.NextDateReset)
	}

	return KiroQuotaBreakdown{
		ID:           fmt.Sprintf("%s_%d_free", entry.ResourceType, idx),
		Label:        label,
		ResourceType: entry.ResourceType,
		Limit:        freeLimit,
		CurrentUsage: freeUsage,
		Remaining:    freeRemaining,
		IsFreeTrial:  true,
		ExpiresAt:    expiresAt,
		NextReset:    nextReset,
	}
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
