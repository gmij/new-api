package kiro

import (
	"encoding/json"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/google/uuid"
)

// convertClaudeToKiro transforms a standard Claude Messages API request into
// the Kiro conversationState format expected by AWS CodeWhisperer.
func convertClaudeToKiro(claudeReq *dto.ClaudeRequest, region string) (*KiroRequest, error) {
	kiroModelID := getKiroModelID(claudeReq.Model)
	conversationID := uuid.New().String()

	// ── Build tool list ──
	tools, err := buildKiroTools(claudeReq.Tools)
	if err != nil {
		return nil, err
	}

	// ── Build system prompt + history + current message from Claude messages ──
	systemPrompt := extractSystemPrompt(claudeReq)
	history, currentContent := splitHistoryAndCurrent(claudeReq.Messages)

	// Prepend system prompt as the first user message if present.
	if systemPrompt != "" {
		currentContent = systemPrompt + "\n\n" + currentContent
	}

	var ctx *UserInputMessageContext
	if len(tools) > 0 {
		ctx = &UserInputMessageContext{Tools: tools}
	}

	kiroReq := &KiroRequest{
		ConversationState: &ConversationState{
			ConversationID: conversationID,
			AgentTaskType:  "vibe",
			CurrentMessage: &CurrentMessage{
				UserInputMessage: &UserInputMessage{
					Content:                 currentContent,
					ModelID:                 kiroModelID,
					Origin:                  "AI_EDITOR",
					ChatTriggerType:         "MANUAL",
					UserInputMessageContext: ctx,
				},
			},
			History: history,
		},
	}

	return kiroReq, nil
}

// extractSystemPrompt returns the system prompt as a plain string.
func extractSystemPrompt(req *dto.ClaudeRequest) string {
	if req.System == nil {
		return ""
	}
	if req.IsStringSystem() {
		return req.GetStringSystem()
	}
	// For structured system blocks, concatenate text parts.
	systemParts, err := common.Any2Type[[]dto.ClaudeMediaMessage](req.System)
	if err != nil {
		return ""
	}
	var parts []string
	for _, p := range systemParts {
		if p.Type == "text" && p.GetText() != "" {
			parts = append(parts, p.GetText())
		}
	}
	return strings.Join(parts, "\n")
}

// splitHistoryAndCurrent separates Claude messages into Kiro history entries
// and the final user message content string.
func splitHistoryAndCurrent(messages []dto.ClaudeMessage) ([]HistoryMessage, string) {
	if len(messages) == 0 {
		return nil, ""
	}

	// The last user message becomes currentMessage.
	lastIdx := len(messages) - 1
	currentContent := messages[lastIdx].GetStringContent()

	// All preceding messages become history.
	var history []HistoryMessage
	for i := 0; i < lastIdx; i++ {
		msg := messages[i]
		switch msg.Role {
		case "user":
			history = append(history, HistoryMessage{
				UserInputMessage: &UserInputMessage{
					Content: msg.GetStringContent(),
				},
			})
		case "assistant":
			history = append(history, HistoryMessage{
				AssistantResponseMessage: &AssistantResponseMessage{
					Content: msg.GetStringContent(),
				},
			})
		}
	}

	return history, currentContent
}

// buildKiroTools converts Claude tools (any type) into Kiro toolSpecification format.
func buildKiroTools(claudeTools any) ([]KiroTool, error) {
	if claudeTools == nil {
		return nil, nil
	}

	// claudeTools can be []any (from JSON unmarshalling)
	toolSlice, ok := claudeTools.([]any)
	if !ok {
		return nil, nil
	}

	var kiroTools []KiroTool
	for _, raw := range toolSlice {
		toolMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		// Skip non-standard tools (e.g. web_search_tool)
		if _, hasType := toolMap["type"]; hasType {
			continue
		}

		name, _ := toolMap["name"].(string)
		desc, _ := toolMap["description"].(string)
		if name == "" {
			continue
		}

		var inputSchema *ToolInputSchema
		if schema, ok := toolMap["input_schema"]; ok && schema != nil {
			schemaJSON, err := json.Marshal(schema)
			if err == nil {
				inputSchema = &ToolInputSchema{JSON: schemaJSON}
			}
		}

		kiroTools = append(kiroTools, KiroTool{
			ToolSpecification: &ToolSpecification{
				Name:        name,
				Description: desc,
				InputSchema: inputSchema,
			},
		})
	}

	return kiroTools, nil
}
