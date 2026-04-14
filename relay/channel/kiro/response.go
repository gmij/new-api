package kiro

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/relay/channel/claude"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// eventStreamMessage represents a single parsed AWS Event Stream frame.
type eventStreamMessage struct {
	EventType   string
	ContentType string
	MessageType string
	Payload     []byte
}

// parseEventStreamFrame attempts to read one AWS Event Stream message from buf
// starting at offset. Returns the parsed message, bytes consumed, or an error.
//
// AWS Event Stream binary format:
//
//	Prelude  (12 bytes): [4B total_length][4B headers_length][4B prelude_crc]
//	Headers  (N bytes):  repeated { 1B name_len, name, 1B type, 2B value_len, value }
//	Payload  (M bytes):  raw bytes
//	Message CRC (4 bytes)
func parseEventStreamFrame(buf []byte, offset int) (*eventStreamMessage, int, error) {
	remaining := len(buf) - offset
	if remaining < 12 {
		return nil, 0, io.ErrShortBuffer
	}

	totalLength := int(binary.BigEndian.Uint32(buf[offset:]))
	headersLength := int(binary.BigEndian.Uint32(buf[offset+4:]))
	// preludeCRC at buf[offset+8 : offset+12] – we skip validation for simplicity

	if totalLength < 16 { // minimum: 12 prelude + 4 message CRC
		return nil, 0, fmt.Errorf("kiro: invalid event stream frame total_length=%d", totalLength)
	}
	if remaining < totalLength {
		return nil, 0, io.ErrShortBuffer
	}

	// Validate message CRC (CRC-32C of entire frame excluding the last 4 bytes).
	messageCRC := binary.BigEndian.Uint32(buf[offset+totalLength-4:])
	computed := crc32.Checksum(buf[offset:offset+totalLength-4], crc32.MakeTable(crc32.Castagnoli))
	if messageCRC != computed {
		return nil, 0, fmt.Errorf("kiro: event stream CRC mismatch")
	}

	// Parse headers.
	msg := &eventStreamMessage{}
	headerEnd := offset + 12 + headersLength
	hdrOffset := offset + 12
	for hdrOffset < headerEnd {
		if hdrOffset >= len(buf) {
			break
		}
		nameLen := int(buf[hdrOffset])
		hdrOffset++
		if hdrOffset+nameLen > headerEnd {
			break
		}
		headerName := string(buf[hdrOffset : hdrOffset+nameLen])
		hdrOffset += nameLen

		if hdrOffset >= headerEnd {
			break
		}
		valueType := buf[hdrOffset]
		hdrOffset++

		if valueType == 7 { // string type
			if hdrOffset+2 > headerEnd {
				break
			}
			valueLen := int(binary.BigEndian.Uint16(buf[hdrOffset:]))
			hdrOffset += 2
			if hdrOffset+valueLen > headerEnd {
				break
			}
			headerValue := string(buf[hdrOffset : hdrOffset+valueLen])
			hdrOffset += valueLen

			switch headerName {
			case ":event-type":
				msg.EventType = headerValue
			case ":content-type":
				msg.ContentType = headerValue
			case ":message-type":
				msg.MessageType = headerValue
			}
		} else {
			// For non-string types, skip: 2B length + value
			if hdrOffset+2 > headerEnd {
				break
			}
			valueLen := int(binary.BigEndian.Uint16(buf[hdrOffset:]))
			hdrOffset += 2 + valueLen
		}
	}

	// Extract payload.
	payloadStart := offset + 12 + headersLength
	payloadEnd := offset + totalLength - 4
	if payloadEnd > payloadStart {
		msg.Payload = buf[payloadStart:payloadEnd]
	}

	return msg, totalLength, nil
}

// kiroStreamHandler reads the AWS Event Stream binary response and converts it
// to Claude SSE events for the client.
func kiroStreamHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	info.FinalRequestRelayFormat = types.RelayFormatClaude

	claudeInfo := &claude.ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	// Read the entire binary body (AWS Event Stream is not chunked SSE).
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("kiro: read response body: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	// Track tool_use state for aggregation.
	type pendingToolUse struct {
		id    string
		name  string
		input strings.Builder
	}
	var currentToolUse *pendingToolUse
	var thinkingBuf strings.Builder

	// Parse frames.
	offset := 0
	for offset < len(body) {
		msg, consumed, parseErr := parseEventStreamFrame(body, offset)
		if parseErr != nil {
			if parseErr == io.ErrShortBuffer {
				break // incomplete frame at end
			}
			break // skip malformed frames
		}
		offset += consumed

		if msg.MessageType == "exception" {
			// Surface the error payload as a Claude error event.
			errData := fmt.Sprintf(`{"type":"error","error":{"type":"server_error","message":%q}}`, string(msg.Payload))
			respErr := claude.HandleStreamResponseData(c, info, claudeInfo, errData)
			if respErr != nil {
				return nil, respErr
			}
			continue
		}

		switch msg.EventType {
		case "assistantResponseEvent":
			var evt KiroAssistantResponseEvent
			if err := common.Unmarshal(msg.Payload, &evt); err != nil {
				continue
			}
			if evt.Content == "" {
				continue
			}

			// Flush any pending tool use before emitting text.
			if currentToolUse != nil {
				emitToolUseBlock(c, info, claudeInfo, currentToolUse.id, currentToolUse.name, currentToolUse.input.String())
				currentToolUse = nil
			}

			claudeInfo.ResponseText.WriteString(evt.Content)
			// Emit as a Claude content_block_delta text event.
			sseData := fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%s}}`,
				mustMarshalString(evt.Content))
			respErr := claude.HandleStreamResponseData(c, info, claudeInfo, sseData)
			if respErr != nil {
				return nil, respErr
			}

		case "reasoningContentEvent":
			var evt KiroReasoningContentEvent
			if err := common.Unmarshal(msg.Payload, &evt); err != nil {
				continue
			}
			if evt.Text == "" {
				continue
			}
			thinkingBuf.WriteString(evt.Text)
			sseData := fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":%s}}`,
				mustMarshalString(evt.Text))
			respErr := claude.HandleStreamResponseData(c, info, claudeInfo, sseData)
			if respErr != nil {
				return nil, respErr
			}

		case "toolUseEvent":
			var evt KiroToolUseEvent
			if err := common.Unmarshal(msg.Payload, &evt); err != nil {
				continue
			}
			if evt.Stop {
				// Flush accumulated tool use.
				if currentToolUse != nil {
					emitToolUseBlock(c, info, claudeInfo, currentToolUse.id, currentToolUse.name, currentToolUse.input.String())
					currentToolUse = nil
				}
				continue
			}
			if evt.Name != "" && evt.ToolUseID != "" {
				// New tool use starts.
				if currentToolUse != nil {
					emitToolUseBlock(c, info, claudeInfo, currentToolUse.id, currentToolUse.name, currentToolUse.input.String())
				}
				currentToolUse = &pendingToolUse{id: evt.ToolUseID, name: evt.Name}
			}
			if evt.Input != "" && currentToolUse != nil {
				currentToolUse.input.WriteString(evt.Input)
			}

		case "meteringEvent":
			var evt KiroMeteringEvent
			if err := common.Unmarshal(msg.Payload, &evt); err != nil {
				continue
			}
			// Map metering to usage. Kiro reports tokens via this event.
			if strings.Contains(strings.ToLower(evt.Unit), "input") {
				claudeInfo.Usage.PromptTokens = evt.Usage
			} else if strings.Contains(strings.ToLower(evt.Unit), "output") {
				claudeInfo.Usage.CompletionTokens = evt.Usage
			}
		}
	}

	// Flush any remaining tool use.
	if currentToolUse != nil {
		emitToolUseBlock(c, info, claudeInfo, currentToolUse.id, currentToolUse.name, currentToolUse.input.String())
	}

	claude.HandleStreamFinalResponse(c, info, claudeInfo)
	return claudeInfo.Usage, nil
}

// kiroHandler handles a non-streaming Kiro response (still AWS Event Stream binary).
func kiroHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*dto.Usage, *types.NewAPIError) {
	info.FinalRequestRelayFormat = types.RelayFormatClaude

	claudeInfo := &claude.ClaudeResponseInfo{
		ResponseId:   helper.GetResponseID(c),
		Created:      common.GetTimestamp(),
		Model:        info.UpstreamModelName,
		ResponseText: strings.Builder{},
		Usage:        &dto.Usage{},
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(fmt.Errorf("kiro: read response body: %w", err), types.ErrorCodeBadResponseBody, http.StatusBadGateway)
	}

	// Parse all frames to collect the full response.
	var fullText strings.Builder
	offset := 0
	for offset < len(body) {
		msg, consumed, parseErr := parseEventStreamFrame(body, offset)
		if parseErr != nil {
			if parseErr == io.ErrShortBuffer {
				break
			}
			break
		}
		offset += consumed

		switch msg.EventType {
		case "assistantResponseEvent":
			var evt KiroAssistantResponseEvent
			if err := common.Unmarshal(msg.Payload, &evt); err == nil {
				fullText.WriteString(evt.Content)
			}
		case "meteringEvent":
			var evt KiroMeteringEvent
			if err := common.Unmarshal(msg.Payload, &evt); err == nil {
				if strings.Contains(strings.ToLower(evt.Unit), "input") {
					claudeInfo.Usage.PromptTokens = evt.Usage
				} else if strings.Contains(strings.ToLower(evt.Unit), "output") {
					claudeInfo.Usage.CompletionTokens = evt.Usage
				}
			}
		}
	}

	claudeInfo.ResponseText = fullText
	claudeInfo.Usage.TotalTokens = claudeInfo.Usage.PromptTokens + claudeInfo.Usage.CompletionTokens

	// Build a Claude-format full response and delegate to the standard handler.
	claudeResponse := buildClaudeFullResponse(claudeInfo)
	respBody, _ := common.Marshal(claudeResponse)
	handleErr := claude.HandleClaudeResponseData(c, info, claudeInfo, resp, respBody)
	if handleErr != nil {
		return nil, handleErr
	}

	return claudeInfo.Usage, nil
}

// buildClaudeFullResponse constructs a dto.ClaudeResponse from collected data.
func buildClaudeFullResponse(info *claude.ClaudeResponseInfo) *dto.ClaudeResponse {
	text := info.ResponseText.String()
	return &dto.ClaudeResponse{
		Id:    info.ResponseId,
		Type:  "message",
		Role:  "assistant",
		Model: info.Model,
		Content: []dto.ClaudeMediaMessage{
			{
				Type: "text",
				Text: &text,
			},
		},
		StopReason: "end_turn",
		Usage: &dto.ClaudeUsage{
			InputTokens:  info.Usage.PromptTokens,
			OutputTokens: info.Usage.CompletionTokens,
		},
	}
}

// emitToolUseBlock sends a tool_use content block as a Claude stream event.
func emitToolUseBlock(c *gin.Context, info *relaycommon.RelayInfo, claudeInfo *claude.ClaudeResponseInfo, id, name, inputJSON string) {
	sseData := fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":%s,"name":%s,"input":%s}}`,
		mustMarshalString(id), mustMarshalString(name), inputJSON)
	_ = claude.HandleStreamResponseData(c, info, claudeInfo, sseData)
}

// mustMarshalString JSON-encodes a string (with proper escaping).
func mustMarshalString(s string) string {
	b, err := common.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
