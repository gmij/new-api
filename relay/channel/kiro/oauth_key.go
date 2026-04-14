package kiro

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
)

// OAuthKey represents the JSON credential stored as the channel's API key.
type OAuthKey struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	AuthMethod   string `json:"auth_method,omitempty"` // "social" or "IdC"
	Region       string `json:"region,omitempty"`       // e.g. "us-east-1"
}

// ParseOAuthKey deserializes the JSON key blob into an OAuthKey struct.
func ParseOAuthKey(raw string) (*OAuthKey, error) {
	if raw == "" {
		return nil, errors.New("kiro channel: empty oauth key")
	}
	var key OAuthKey
	if err := common.Unmarshal([]byte(raw), &key); err != nil {
		return nil, errors.New("kiro channel: invalid oauth key json")
	}
	if key.AccessToken == "" {
		return nil, errors.New("kiro channel: access_token is required")
	}
	if key.Region == "" {
		key.Region = kiroDefaultRegion
	}
	return &key, nil
}
