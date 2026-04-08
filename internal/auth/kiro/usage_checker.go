// Package kiro provides authentication functionality for AWS CodeWhisperer (Kiro) API.
// This file implements usage quota checking and monitoring.
package kiro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

// UsageQuotaResponse represents the API response structure for usage quota checking.
type UsageQuotaResponse struct {
	UsageBreakdownList []UsageBreakdownExtended `json:"usageBreakdownList"`
	SubscriptionInfo   *SubscriptionInfo        `json:"subscriptionInfo,omitempty"`
	NextDateReset      float64                  `json:"nextDateReset,omitempty"`
}

// KiroCreditsResponse represents the getUserCredits API response in a normalized form.
type KiroCreditsResponse struct {
	RemainingCredits float64
	TotalCredits     float64
	HasTotalCredits  bool
	ResetDate        *time.Time
	SubscriptionType string
}

// UsageBreakdownExtended represents detailed usage information for quota checking.
// Note: UsageBreakdown is already defined in codewhisperer_client.go
type UsageBreakdownExtended struct {
	UsageBreakdown
	UsageLimitWithPrecision   float64                `json:"usageLimitWithPrecision"`
	CurrentUsageWithPrecision float64                `json:"currentUsageWithPrecision"`
	FreeTrialInfo             *FreeTrialInfoExtended `json:"freeTrialInfo,omitempty"`
}

// FreeTrialInfoExtended represents free trial usage information.
type FreeTrialInfoExtended struct {
	FreeTrialStatus           string   `json:"freeTrialStatus"`
	FreeTrialExpiry           *float64 `json:"freeTrialExpiry,omitempty"`
	UsageLimitWithPrecision   float64  `json:"usageLimitWithPrecision"`
	CurrentUsageWithPrecision float64  `json:"currentUsageWithPrecision"`
	NextDateReset             *float64 `json:"nextDateReset,omitempty"`
}

// QuotaStatus represents the quota status for a token.
type QuotaStatus struct {
	TotalLimit     float64
	CurrentUsage   float64
	RemainingQuota float64
	IsExhausted    bool
	ResourceType   string
	NextReset      time.Time
}

// UsageChecker provides methods for checking token quota usage.
type UsageChecker struct {
	httpClient *http.Client
}

func parseUnixTimestampAuto(value float64) time.Time {
	if value <= 0 {
		return time.Time{}
	}

	seconds := value
	switch {
	case value >= 1e18:
		seconds = value / 1e9
	case value >= 1e15:
		seconds = value / 1e6
	case value >= 1e12:
		seconds = value / 1e3
	}

	secPart := int64(seconds)
	nanoPart := int64((seconds - float64(secPart)) * float64(time.Second))
	if nanoPart < 0 {
		nanoPart = 0
	}

	return time.Unix(secPart, nanoPart)
}

func ParseUnixTimestamp(value *float64) *time.Time {
	if value == nil {
		return nil
	}
	t := parseUnixTimestampAuto(*value)
	if t.IsZero() {
		return nil
	}
	return &t
}

func parseFlexibleTimeValue(value any) *time.Time {
	switch typed := value.(type) {
	case nil:
		return nil
	case float64:
		return ParseUnixTimestamp(&typed)
	case float32:
		v := float64(typed)
		return ParseUnixTimestamp(&v)
	case int64:
		v := float64(typed)
		return ParseUnixTimestamp(&v)
	case int:
		v := float64(typed)
		return ParseUnixTimestamp(&v)
	case json.Number:
		if f, err := typed.Float64(); err == nil {
			return ParseUnixTimestamp(&f)
		}
	case string:
		if typed == "" {
			return nil
		}
		if f, err := strconv.ParseFloat(typed, 64); err == nil {
			return ParseUnixTimestamp(&f)
		}
		if t, err := time.Parse(time.RFC3339, typed); err == nil {
			return &t
		}
		if t, err := time.Parse("2006-01-02T15:04:05.000Z", typed); err == nil {
			return &t
		}
	}

	return nil
}

func parseFloatValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case json.Number:
		v, err := typed.Float64()
		return v, err == nil
	case string:
		if typed == "" {
			return 0, false
		}
		v, err := strconv.ParseFloat(typed, 64)
		return v, err == nil
	default:
		return 0, false
	}
}

func parseStringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", typed)
	}
}

// NewUsageChecker creates a new UsageChecker instance.
func NewUsageChecker(cfg *config.Config) *UsageChecker {
	return &UsageChecker{
		httpClient: util.SetProxy(&cfg.SDKConfig, &http.Client{Timeout: 30 * time.Second}),
	}
}

// NewUsageCheckerWithClient creates a UsageChecker with a custom HTTP client.
func NewUsageCheckerWithClient(client *http.Client) *UsageChecker {
	return &UsageChecker{
		httpClient: client,
	}
}

// CheckUsage retrieves usage limits for the given token.
func (c *UsageChecker) CheckUsage(ctx context.Context, tokenData *KiroTokenData) (*UsageQuotaResponse, error) {
	if tokenData == nil {
		return nil, fmt.Errorf("token data is nil")
	}

	if tokenData.AccessToken == "" {
		return nil, fmt.Errorf("access token is empty")
	}

	queryParams := map[string]string{
		"origin":       "AI_EDITOR",
		"profileArn":   tokenData.ProfileArn,
		"resourceType": "AGENTIC_REQUEST",
	}

	// Use endpoint from profileArn if available
	endpoint := GetKiroAPIEndpointFromProfileArn(tokenData.ProfileArn)
	url := buildURL(endpoint, pathGetUsageLimits, queryParams)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	accountKey := GetAccountKey(tokenData.ClientID, tokenData.RefreshToken)
	setRuntimeHeaders(req, tokenData.AccessToken, accountKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}

	var result UsageQuotaResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse usage response: %w", err)
	}

	return &result, nil
}

// CheckCredits retrieves Kiro credits using the same endpoint pattern as Kiro IDE and OmniRoute.
func (c *UsageChecker) CheckCredits(ctx context.Context, tokenData *KiroTokenData) (*KiroCreditsResponse, error) {
	if tokenData == nil {
		return nil, fmt.Errorf("token data is nil")
	}
	if tokenData.AccessToken == "" {
		return nil, fmt.Errorf("access token is empty")
	}

	url := buildURL(GetCodeWhispererLegacyEndpoint("us-east-1"), "getUserCredits", nil)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create credits request: %w", err)
	}

	accountKey := GetAccountKey(tokenData.ClientID, tokenData.RefreshToken)
	setRuntimeHeaders(req, tokenData.AccessToken, accountKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("credits request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read credits response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("credits API error (status %d): %s", resp.StatusCode, string(body))
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("failed to parse credits response: %w", err)
	}

	result := &KiroCreditsResponse{
		SubscriptionType: parseStringValue(raw["subscriptionType"]),
	}
	if result.SubscriptionType == "" {
		result.SubscriptionType = parseStringValue(raw["subscription_type"])
	}

	if remaining, ok := parseFloatValue(raw["remainingCredits"]); ok {
		result.RemainingCredits = remaining
	} else if remaining, ok := parseFloatValue(raw["remaining_credits"]); ok {
		result.RemainingCredits = remaining
	}

	if total, ok := parseFloatValue(raw["totalCredits"]); ok {
		result.TotalCredits = total
		result.HasTotalCredits = true
	} else if total, ok := parseFloatValue(raw["total_credits"]); ok {
		result.TotalCredits = total
		result.HasTotalCredits = true
	}

	result.ResetDate = parseFlexibleTimeValue(raw["resetDate"])
	if result.ResetDate == nil {
		result.ResetDate = parseFlexibleTimeValue(raw["reset_date"])
	}

	return result, nil
}

// CheckUsageByAccessToken retrieves usage limits using an access token and profile ARN directly.
func (c *UsageChecker) CheckUsageByAccessToken(ctx context.Context, accessToken, profileArn string) (*UsageQuotaResponse, error) {
	tokenData := &KiroTokenData{
		AccessToken: accessToken,
		ProfileArn:  profileArn,
	}
	return c.CheckUsage(ctx, tokenData)
}

// GetRemainingQuota calculates the remaining quota from usage limits.
func GetRemainingQuota(usage *UsageQuotaResponse) float64 {
	if usage == nil || len(usage.UsageBreakdownList) == 0 {
		return 0
	}

	var totalRemaining float64
	for _, breakdown := range usage.UsageBreakdownList {
		remaining := breakdown.UsageLimitWithPrecision - breakdown.CurrentUsageWithPrecision
		if remaining > 0 {
			totalRemaining += remaining
		}

		if breakdown.FreeTrialInfo != nil {
			freeRemaining := breakdown.FreeTrialInfo.UsageLimitWithPrecision - breakdown.FreeTrialInfo.CurrentUsageWithPrecision
			if freeRemaining > 0 {
				totalRemaining += freeRemaining
			}
		}
	}

	return totalRemaining
}

// IsQuotaExhausted checks if the quota is exhausted based on usage limits.
func IsQuotaExhausted(usage *UsageQuotaResponse) bool {
	if usage == nil || len(usage.UsageBreakdownList) == 0 {
		return true
	}

	for _, breakdown := range usage.UsageBreakdownList {
		if breakdown.CurrentUsageWithPrecision < breakdown.UsageLimitWithPrecision {
			return false
		}

		if breakdown.FreeTrialInfo != nil {
			if breakdown.FreeTrialInfo.CurrentUsageWithPrecision < breakdown.FreeTrialInfo.UsageLimitWithPrecision {
				return false
			}
		}
	}

	return true
}

// GetQuotaStatus retrieves a comprehensive quota status for a token.
func (c *UsageChecker) GetQuotaStatus(ctx context.Context, tokenData *KiroTokenData) (*QuotaStatus, error) {
	usage, err := c.CheckUsage(ctx, tokenData)
	if err != nil {
		return nil, err
	}
	return BuildQuotaStatus(usage), nil
}

// BuildQuotaStatus builds quota summary from existing UsageQuotaResponse.
func BuildQuotaStatus(usage *UsageQuotaResponse) *QuotaStatus {
	status := &QuotaStatus{IsExhausted: true}
	if usage == nil {
		return status
	}

	status.IsExhausted = IsQuotaExhausted(usage)

	if len(usage.UsageBreakdownList) > 0 {
		breakdown := usage.UsageBreakdownList[0]
		status.TotalLimit = breakdown.UsageLimitWithPrecision
		status.CurrentUsage = breakdown.CurrentUsageWithPrecision
		status.RemainingQuota = breakdown.UsageLimitWithPrecision - breakdown.CurrentUsageWithPrecision
		status.ResourceType = breakdown.ResourceType

		if breakdown.FreeTrialInfo != nil {
			status.TotalLimit += breakdown.FreeTrialInfo.UsageLimitWithPrecision
			status.CurrentUsage += breakdown.FreeTrialInfo.CurrentUsageWithPrecision
			freeRemaining := breakdown.FreeTrialInfo.UsageLimitWithPrecision - breakdown.FreeTrialInfo.CurrentUsageWithPrecision
			if freeRemaining > 0 {
				status.RemainingQuota += freeRemaining
			}
		}
	}

	if usage.NextDateReset > 0 {
		status.NextReset = parseUnixTimestampAuto(usage.NextDateReset)
	}

	return status
}

// CalculateAvailableCount calculates the available request count based on usage limits.
func CalculateAvailableCount(usage *UsageQuotaResponse) float64 {
	return GetRemainingQuota(usage)
}

// GetUsagePercentage calculates the usage percentage.
func GetUsagePercentage(usage *UsageQuotaResponse) float64 {
	if usage == nil || len(usage.UsageBreakdownList) == 0 {
		return 100.0
	}

	var totalLimit, totalUsage float64
	for _, breakdown := range usage.UsageBreakdownList {
		totalLimit += breakdown.UsageLimitWithPrecision
		totalUsage += breakdown.CurrentUsageWithPrecision

		if breakdown.FreeTrialInfo != nil {
			totalLimit += breakdown.FreeTrialInfo.UsageLimitWithPrecision
			totalUsage += breakdown.FreeTrialInfo.CurrentUsageWithPrecision
		}
	}

	if totalLimit == 0 {
		return 100.0
	}

	return (totalUsage / totalLimit) * 100
}
