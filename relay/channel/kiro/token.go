package kiro

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/service"
)

const (
	expireWindowMS     = 5 * 60 * 1000   // 5 minutes before expiry
	refreshDebounceMS  = 30 * 1000       // 30 seconds between refresh attempts
)

// refreshState tracks the per-refreshToken debounce and in-flight state.
type refreshState struct {
	lastAttempt time.Time
	mu          sync.Mutex
}

var (
	refreshStates sync.Map // map[string]*refreshState keyed by refreshToken
)

// RefreshResult contains the updated credentials after a successful refresh.
type RefreshResult struct {
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresIn    int64  `json:"expiresIn,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
	ProfileArn   string `json:"profileArn,omitempty"`
}

// NeedsRefresh returns true if the token expires within the next 5 minutes.
func NeedsRefresh(expiresAtStr string) bool {
	if expiresAtStr == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, expiresAtStr)
	if err != nil {
		return false
	}
	return time.Until(expiresAt).Milliseconds() <= expireWindowMS
}

// RefreshAccessToken refreshes the access token using the appropriate method.
// It implements debouncing (30s) and concurrency protection per refresh token.
func RefreshAccessToken(key *OAuthKey) (*RefreshResult, error) {
	if key.RefreshToken == "" {
		return nil, fmt.Errorf("kiro: no refresh token available")
	}

	// Get or create debounce state.
	stateI, _ := refreshStates.LoadOrStore(key.RefreshToken, &refreshState{})
	state := stateI.(*refreshState)

	state.mu.Lock()
	defer state.mu.Unlock()

	// Debounce: skip if last attempt was < 30s ago.
	if time.Since(state.lastAttempt).Milliseconds() < refreshDebounceMS {
		return nil, fmt.Errorf("kiro: refresh debounced (last attempt %s ago)", time.Since(state.lastAttempt).Round(time.Second))
	}
	state.lastAttempt = time.Now()

	region := key.Region
	if region == "" {
		region = kiroDefaultRegion
	}

	var refreshURL string
	var bodyMap map[string]string

	switch key.AuthMethod {
	case AuthMethodSocial, "":
		refreshURL = fmt.Sprintf(kiroRefreshSocialURL, region)
		bodyMap = map[string]string{
			"refreshToken": key.RefreshToken,
		}
	case AuthMethodIdC:
		refreshURL = fmt.Sprintf(kiroRefreshIdCURL, region)
		bodyMap = map[string]string{
			"refreshToken": key.RefreshToken,
			"clientId":     key.ClientID,
			"clientSecret": key.ClientSecret,
			"grantType":    "refresh_token",
		}
	default:
		return nil, fmt.Errorf("kiro: unsupported auth method %q", key.AuthMethod)
	}

	bodyJSON, err := common.Marshal(bodyMap)
	if err != nil {
		return nil, fmt.Errorf("kiro: marshal refresh body: %w", err)
	}

	httpClient := service.GetHttpClient()
	req, err := http.NewRequest(http.MethodPost, refreshURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("kiro: create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("kiro: refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("kiro: read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		common.SysError(fmt.Sprintf("kiro: token refresh returned %d: %s", resp.StatusCode, string(respBody)))
		return nil, fmt.Errorf("kiro: refresh returned status %d", resp.StatusCode)
	}

	var result RefreshResult
	if err := common.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("kiro: unmarshal refresh response: %w", err)
	}

	if result.AccessToken == "" {
		return nil, fmt.Errorf("kiro: refresh response missing accessToken")
	}

	// Compute expiresAt if only expiresIn was returned.
	if result.ExpiresAt == "" && result.ExpiresIn > 0 {
		result.ExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	} else if result.ExpiresAt == "" {
		// Default to 1 hour.
		result.ExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	}

	return &result, nil
}

// MergeRefreshResult applies a RefreshResult to the OAuthKey, returning the
// updated key and whether any field changed.
func MergeRefreshResult(key *OAuthKey, result *RefreshResult) (updated *OAuthKey, changed bool) {
	updated = &OAuthKey{
		AccessToken:  result.AccessToken,
		RefreshToken: key.RefreshToken,
		ExpiresAt:    result.ExpiresAt,
		ClientID:     key.ClientID,
		ClientSecret: key.ClientSecret,
		AuthMethod:   key.AuthMethod,
		Region:       key.Region,
	}
	if result.RefreshToken != "" {
		updated.RefreshToken = result.RefreshToken
	}
	changed = (updated.AccessToken != key.AccessToken ||
		updated.RefreshToken != key.RefreshToken ||
		updated.ExpiresAt != key.ExpiresAt)
	return updated, changed
}
