package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
)

type KiroCredentialRefreshOptions struct {
	ResetCaches bool
}

// KiroOAuthKey mirrors relay/channel/kiro.OAuthKey to avoid importing the
// relay package from the service layer (which would create an import cycle).
type KiroOAuthKey struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	AuthMethod   string `json:"auth_method,omitempty"`
	Region       string `json:"region,omitempty"`
}

const (
	kiroDefaultRegion    = "us-east-1"
	kiroAuthMethodSocial = "social"
	kiroAuthMethodIdC    = "IdC"
	kiroRefreshSocialURL = "https://prod.%s.auth.desktop.kiro.dev/refreshToken"
	kiroRefreshIdCURL    = "https://oidc.%s.amazonaws.com/token"
)

// ParseKiroOAuthKeyPublic is the exported version of parseKiroOAuthKey,
// used by controllers that need to inspect the key without refreshing.
func ParseKiroOAuthKeyPublic(raw string) (*KiroOAuthKey, error) {
	return parseKiroOAuthKey(raw)
}

func parseKiroOAuthKey(raw string) (*KiroOAuthKey, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("kiro channel: empty oauth key")
	}
	var key KiroOAuthKey
	if err := common.Unmarshal([]byte(raw), &key); err != nil {
		return nil, fmt.Errorf("kiro channel: invalid oauth key json")
	}
	if strings.TrimSpace(key.AccessToken) == "" {
		return nil, fmt.Errorf("kiro channel: access_token is required")
	}
	if key.Region == "" {
		key.Region = kiroDefaultRegion
	}
	return &key, nil
}

// kiroRefreshResult matches the token refresh response from the Kiro auth endpoints.
type kiroRefreshResult struct {
	AccessToken  string `json:"accessToken,omitempty"`
	RefreshToken string `json:"refreshToken,omitempty"`
	ExpiresIn    int64  `json:"expiresIn,omitempty"`
	ExpiresAt    string `json:"expiresAt,omitempty"`
}

func refreshKiroToken(ctx context.Context, key *KiroOAuthKey) (*kiroRefreshResult, error) {
	if strings.TrimSpace(key.RefreshToken) == "" {
		return nil, fmt.Errorf("kiro: no refresh token available")
	}

	region := key.Region
	if region == "" {
		region = kiroDefaultRegion
	}

	var refreshURL string
	var bodyMap map[string]string

	switch key.AuthMethod {
	case kiroAuthMethodSocial, "":
		refreshURL = fmt.Sprintf(kiroRefreshSocialURL, region)
		bodyMap = map[string]string{
			"refreshToken": key.RefreshToken,
		}
	case kiroAuthMethodIdC:
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

	httpClient := GetHttpClient()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, refreshURL, bytes.NewReader(bodyJSON))
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
		return nil, fmt.Errorf("kiro: refresh returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var result kiroRefreshResult
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
		result.ExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	}

	return &result, nil
}

func RefreshKiroChannelCredential(ctx context.Context, channelID int, opts KiroCredentialRefreshOptions) (*KiroOAuthKey, *model.Channel, error) {
	ch, err := model.GetChannelById(channelID, true)
	if err != nil {
		return nil, nil, err
	}
	if ch == nil {
		return nil, nil, fmt.Errorf("channel not found")
	}
	if ch.Type != constant.ChannelTypeKiro {
		return nil, nil, fmt.Errorf("channel type is not Kiro")
	}

	oauthKey, err := parseKiroOAuthKey(strings.TrimSpace(ch.Key))
	if err != nil {
		return nil, nil, err
	}
	if strings.TrimSpace(oauthKey.RefreshToken) == "" {
		return nil, nil, fmt.Errorf("kiro channel: refresh_token is required to refresh credential")
	}

	refreshCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	result, err := refreshKiroToken(refreshCtx, oauthKey)
	if err != nil {
		return nil, nil, err
	}

	// Apply refresh result.
	oauthKey.AccessToken = result.AccessToken
	if result.RefreshToken != "" {
		oauthKey.RefreshToken = result.RefreshToken
	}
	oauthKey.ExpiresAt = result.ExpiresAt

	encoded, err := common.Marshal(oauthKey)
	if err != nil {
		return nil, nil, err
	}

	if err := model.DB.Model(&model.Channel{}).Where("id = ?", ch.Id).Update("key", string(encoded)).Error; err != nil {
		return nil, nil, err
	}

	if opts.ResetCaches {
		model.InitChannelCache()
		ResetProxyClientCache()
	}

	return oauthKey, ch, nil
}

// isKiroTokenExpiringSoon checks if the Kiro credential's access_token is
// expiring within the given threshold.
func isKiroTokenExpiringSoon(oauthKey *KiroOAuthKey, threshold time.Duration) bool {
	if oauthKey.ExpiresAt == "" {
		return false
	}
	expiresAt, err := time.Parse(time.RFC3339, oauthKey.ExpiresAt)
	if err != nil {
		return false
	}
	return time.Until(expiresAt) <= threshold
}
