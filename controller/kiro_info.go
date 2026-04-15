package controller

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// GetKiroChannelInfo returns credential status information for a Kiro channel,
// including token expiration, auth method, region, and whether the token has
// been refreshed successfully. On 401/403-equivalent states (e.g. expired
// tokens), it attempts an automatic refresh.
func GetKiroChannelInfo(c *gin.Context) {
	channelId, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiError(c, fmt.Errorf("invalid channel id: %w", err))
		return
	}

	ch, err := model.GetChannelById(channelId, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if ch == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel not found"})
		return
	}
	if ch.Type != constant.ChannelTypeKiro {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "channel type is not Kiro"})
		return
	}
	if ch.ChannelInfo.IsMultiKey {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "multi-key channel is not supported"})
		return
	}

	oauthKey, err := service.ParseKiroOAuthKeyPublic(strings.TrimSpace(ch.Key))
	if err != nil {
		common.SysError("kiro info: failed to parse oauth key: " + err.Error())
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "解析凭证失败，请检查渠道配置"})
		return
	}

	// Compute token status.
	expiresAt := strings.TrimSpace(oauthKey.ExpiresAt)
	hasRefreshToken := strings.TrimSpace(oauthKey.RefreshToken) != ""
	tokenExpired := false
	tokenExpiringSoon := false
	var expiresAtTime *time.Time

	if expiresAt != "" {
		if t, parseErr := time.Parse(time.RFC3339, expiresAt); parseErr == nil {
			expiresAtTime = &t
			tokenExpired = time.Now().After(t)
			tokenExpiringSoon = !tokenExpired && time.Until(t) <= 5*time.Minute
		}
	}

	// If token is expired and we have a refresh_token, attempt auto-refresh.
	refreshed := false
	refreshError := ""
	if (tokenExpired || tokenExpiringSoon) && hasRefreshToken {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()

		newKey, _, refreshErr := service.RefreshKiroChannelCredential(ctx, channelId, service.KiroCredentialRefreshOptions{ResetCaches: true})
		if refreshErr != nil {
			refreshError = refreshErr.Error()
			common.SysError("kiro info: auto-refresh failed: " + refreshError)
		} else {
			refreshed = true
			oauthKey = newKey
			expiresAt = oauthKey.ExpiresAt
			tokenExpired = false
			tokenExpiringSoon = false
			if expiresAt != "" {
				if t, parseErr := time.Parse(time.RFC3339, expiresAt); parseErr == nil {
					expiresAtTime = &t
					tokenExpiringSoon = time.Until(t) <= 5*time.Minute
				}
			}
		}
	}

	// Compute human-readable status.
	status := "active"
	if tokenExpired {
		status = "expired"
	} else if tokenExpiringSoon {
		status = "expiring_soon"
	}

	// Remaining seconds until expiry.
	var remainingSeconds *int64
	if expiresAtTime != nil {
		remaining := int64(time.Until(*expiresAtTime).Seconds())
		if remaining < 0 {
			remaining = 0
		}
		remainingSeconds = &remaining
	}

	data := gin.H{
		"auth_method":       oauthKey.AuthMethod,
		"region":            oauthKey.Region,
		"has_refresh_token": hasRefreshToken,
		"expires_at":        expiresAt,
		"remaining_seconds": remainingSeconds,
		"status":            status,
		"auto_refreshed":    refreshed,
		"channel_id":        ch.Id,
		"channel_name":      ch.Name,
	}
	if refreshError != "" {
		data["refresh_error"] = refreshError
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    data,
	})
}
