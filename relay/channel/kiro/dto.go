package kiro

import "encoding/json"

// ─── Kiro request structures (conversationState format) ───

// KiroRequest is the top-level request body sent to the AWS CodeWhisperer endpoint.
type KiroRequest struct {
	ConversationState *ConversationState `json:"conversationState"`
}

// ConversationState wraps the current message, history, and metadata.
type ConversationState struct {
	ConversationID      string           `json:"conversationId"`
	AgentContinuationID string           `json:"agentContinuationId,omitempty"`
	AgentTaskType       string           `json:"agentTaskType,omitempty"`
	CurrentMessage      *CurrentMessage  `json:"currentMessage"`
	History             []HistoryMessage `json:"history,omitempty"`
}

// CurrentMessage contains the latest user input.
type CurrentMessage struct {
	UserInputMessage *UserInputMessage `json:"userInputMessage"`
}

// UserInputMessage represents a single user turn.
type UserInputMessage struct {
	Content                 string                   `json:"content"`
	ModelID                 string                   `json:"modelId"`
	Origin                  string                   `json:"origin"`
	ChatTriggerType         string                   `json:"chatTriggerType"`
	UserInputMessageContext *UserInputMessageContext  `json:"userInputMessageContext,omitempty"`
}

// UserInputMessageContext carries tool definitions and optional context.
type UserInputMessageContext struct {
	Tools []KiroTool `json:"tools,omitempty"`
}

// KiroTool represents a single tool in the Kiro toolSpecification format.
type KiroTool struct {
	ToolSpecification *ToolSpecification `json:"toolSpecification"`
}

// ToolSpecification defines a tool's name, description, and input schema.
type ToolSpecification struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	InputSchema *ToolInputSchema  `json:"inputSchema,omitempty"`
}

// ToolInputSchema wraps the JSON schema for tool input.
type ToolInputSchema struct {
	JSON json.RawMessage `json:"json,omitempty"`
}

// HistoryMessage is one entry in the conversation history.
// Exactly one of UserInputMessage or AssistantResponseMessage should be set.
type HistoryMessage struct {
	UserInputMessage         *UserInputMessage         `json:"userInputMessage,omitempty"`
	AssistantResponseMessage *AssistantResponseMessage  `json:"assistantResponseMessage,omitempty"`
}

// AssistantResponseMessage represents an assistant reply in history.
type AssistantResponseMessage struct {
	Content  string    `json:"content"`
	ToolUses []ToolUse `json:"toolUses,omitempty"`
}

// ToolUse represents a tool invocation by the assistant.
type ToolUse struct {
	Name      string `json:"name"`
	ToolUseID string `json:"toolUseId"`
	Input     string `json:"input"`
}

// ─── AWS Event Stream response structures ───

// KiroAssistantResponseEvent is the payload of an "assistantResponseEvent".
type KiroAssistantResponseEvent struct {
	Content string `json:"content"`
}

// KiroToolUseEvent is the payload of a "toolUseEvent".
type KiroToolUseEvent struct {
	Name      string `json:"name,omitempty"`
	ToolUseID string `json:"toolUseId,omitempty"`
	Input     string `json:"input,omitempty"`
	Stop      bool   `json:"stop,omitempty"`
}

// KiroReasoningContentEvent is the payload of a "reasoningContentEvent".
type KiroReasoningContentEvent struct {
	Text string `json:"text,omitempty"`
}

// KiroMeteringEvent is the payload of a "meteringEvent".
type KiroMeteringEvent struct {
	Usage int    `json:"usage,omitempty"`
	Unit  string `json:"unit,omitempty"`
}
