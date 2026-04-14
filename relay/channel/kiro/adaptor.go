package kiro

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

type Adaptor struct {
	oauthKey *OAuthKey
}

// ── Unsupported endpoint stubs ──

func (a *Adaptor) ConvertGeminiRequest(*gin.Context, *relaycommon.RelayInfo, *dto.GeminiChatRequest) (any, error) {
	return nil, errors.New("kiro channel: endpoint not supported")
}

func (a *Adaptor) ConvertAudioRequest(*gin.Context, *relaycommon.RelayInfo, dto.AudioRequest) (io.Reader, error) {
	return nil, errors.New("kiro channel: endpoint not supported")
}

func (a *Adaptor) ConvertImageRequest(*gin.Context, *relaycommon.RelayInfo, dto.ImageRequest) (any, error) {
	return nil, errors.New("kiro channel: endpoint not supported")
}

func (a *Adaptor) ConvertRerankRequest(*gin.Context, int, dto.RerankRequest) (any, error) {
	return nil, errors.New("kiro channel: endpoint not supported")
}

func (a *Adaptor) ConvertEmbeddingRequest(*gin.Context, *relaycommon.RelayInfo, dto.EmbeddingRequest) (any, error) {
	return nil, errors.New("kiro channel: endpoint not supported")
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(*gin.Context, *relaycommon.RelayInfo, dto.OpenAIResponsesRequest) (any, error) {
	return nil, errors.New("kiro channel: endpoint not supported")
}

// ── Core implementation ──

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {
	// Parse the JSON key early so other methods can use it.
	key, err := ParseOAuthKey(info.ApiKey)
	if err != nil {
		common.SysError(fmt.Sprintf("kiro: failed to parse key: %v", err))
		return
	}
	a.oauthKey = key
}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	if a.oauthKey == nil {
		return "", errors.New("kiro channel: credentials not initialized")
	}
	region := a.oauthKey.Region
	if region == "" {
		region = kiroDefaultRegion
	}
	// If the user configured a custom base URL, use it; otherwise generate from region.
	if info.ChannelBaseUrl != "" {
		return info.ChannelBaseUrl, nil
	}
	return fmt.Sprintf(kiroGenerateAssistantURL, region), nil
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	if a.oauthKey == nil {
		return errors.New("kiro channel: credentials not initialized")
	}

	// Try to refresh the token if needed.
	if NeedsRefresh(a.oauthKey.ExpiresAt) && a.oauthKey.RefreshToken != "" {
		result, err := RefreshAccessToken(a.oauthKey)
		if err != nil {
			common.SysError(fmt.Sprintf("kiro: token refresh failed (will use existing token): %v", err))
		} else {
			updated, changed := MergeRefreshResult(a.oauthKey, result)
			if changed {
				a.oauthKey = updated
				// Persist the updated key back to the channel (best-effort).
				go persistUpdatedKey(info, updated)
			}
		}
	}

	req.Set("Content-Type", "application/json")
	req.Set("Accept", "application/json")
	req.Set("Authorization", "Bearer "+a.oauthKey.AccessToken)
	req.Set("amz-sdk-invocation-id", common.GetUUID())
	req.Set("x-amzn-kiro-agent-mode", "vibe")
	req.Set("User-Agent", kiroUserAgent)
	req.Set("x-amz-user-agent", kiroAmzUserAgent)

	return nil
}

// ConvertOpenAIRequest converts an OpenAI-format request to Kiro format.
// First converts OpenAI → Claude, then Claude → Kiro.
func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	claudeReq, err := claude.RequestOpenAI2ClaudeMessage(c, *request)
	if err != nil {
		return nil, fmt.Errorf("kiro: openai->claude conversion failed: %w", err)
	}
	return a.convertClaudeReqToKiro(claudeReq)
}

// ConvertClaudeRequest converts a Claude Messages API request to Kiro format.
func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	if request == nil {
		return nil, errors.New("request is nil")
	}
	return a.convertClaudeReqToKiro(request)
}

func (a *Adaptor) convertClaudeReqToKiro(claudeReq *dto.ClaudeRequest) (*KiroRequest, error) {
	region := kiroDefaultRegion
	if a.oauthKey != nil {
		region = a.oauthKey.Region
	}
	return convertClaudeToKiro(claudeReq, region)
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return channel.DoApiRequest(a, c, info, requestBody)
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	// Kiro always returns AWS Event Stream binary, even for non-stream requests.
	// We detect by Content-Type or always use the event stream parser.
	if info.IsStream {
		return kiroStreamHandler(c, resp, info)
	}
	return kiroHandler(c, resp, info)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}

// persistUpdatedKey marshals the updated OAuthKey and persists it as the
// channel's API key. This is a best-effort background operation.
func persistUpdatedKey(info *relaycommon.RelayInfo, key *OAuthKey) {
	keyJSON, err := common.Marshal(key)
	if err != nil {
		common.SysError(fmt.Sprintf("kiro: marshal updated key: %v", err))
		return
	}
	// Update the in-memory copy so subsequent requests in this session use the new token.
	info.ApiKey = string(keyJSON)

	// Persist to DB if a callback has been registered.
	channelID := info.ChannelId
	if channelID <= 0 || UpdateChannelKeyFunc == nil {
		return
	}
	if updateErr := UpdateChannelKeyFunc(channelID, string(keyJSON)); updateErr != nil {
		common.SysError(fmt.Sprintf("kiro: persist updated key for channel %d: %v", channelID, updateErr))
	}
}

// UpdateChannelKeyFunc is a package-level callback for persisting an updated
// channel key to the database. It is set from outside the relay package
// (typically in main or an init function) to avoid import cycles with the
// model layer. When nil, token refreshes are only applied in-memory.
var UpdateChannelKeyFunc func(channelID int, newKey string) error
