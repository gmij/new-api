package kiro

// ModelList contains the Claude models accessible via Kiro (AWS CodeWhisperer).
var ModelList = []string{
	"claude-sonnet-4-20250514",
	"claude-sonnet-4-5-20250929",
	"claude-opus-4-5-20251101",
	"claude-haiku-4-5-20251001",
}

const ChannelName = "kiro"

// kiroModelIDMap maps standard Anthropic model IDs to AWS CodeWhisperer model IDs.
var kiroModelIDMap = map[string]string{
	"claude-opus-4-5":            "claude-opus-4.5",
	"claude-opus-4-5-20251101":   "claude-opus-4.5",
	"claude-haiku-4-5":           "claude-haiku-4.5",
	"claude-haiku-4-5-20251001":  "claude-haiku-4.5",
	"claude-sonnet-4-5":          "CLAUDE_SONNET_4_5_20250929_V1_0",
	"claude-sonnet-4-5-20250929": "CLAUDE_SONNET_4_5_20250929_V1_0",
	"claude-sonnet-4-20250514":   "CLAUDE_SONNET_4_20250514_V1_0",
}

// getKiroModelID returns the Kiro-specific model identifier for the given model name.
// If no mapping exists, the original model name is returned.
func getKiroModelID(model string) string {
	if mapped, ok := kiroModelIDMap[model]; ok {
		return mapped
	}
	return model
}

// URL templates – the region placeholder is replaced at runtime.
const (
	kiroGenerateAssistantURL = "https://codewhisperer.%s.amazonaws.com/generateAssistantResponse"
	kiroRefreshSocialURL     = "https://prod.%s.auth.desktop.kiro.dev/refreshToken"
	kiroRefreshIdCURL        = "https://oidc.%s.amazonaws.com/token"
)

const (
	kiroDefaultRegion = "us-east-1"
	kiroUserAgent     = "aws-sdk-js/3.826.0 ua/2.1 os/win32#10.0.26100 lang/js md/nodejs#22.11.0 api/codewhispererstreaming#3.826.0 m/N,E KiroIDE-0.7.45"
	kiroAmzUserAgent  = "aws-sdk-js/3.826.0 KiroIDE-0.7.45"
)

// Auth method constants matching kiro2Api.
const (
	AuthMethodSocial = "social"
	AuthMethodIdC    = "IdC"
)
