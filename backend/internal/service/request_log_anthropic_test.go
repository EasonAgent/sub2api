package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnthropicResponseAccumulator_BasicTextResponse(t *testing.T) {
	acc := newAnthropicResponseAccumulator()

	// message_start
	acc.ProcessEvent("message_start", []byte(`{
		"type": "message_start",
		"message": {
			"id": "msg_123",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514",
			"usage": {"input_tokens": 100, "cache_read_input_tokens": 50}
		}
	}`))

	// content_block_start
	acc.ProcessEvent("content_block_start", []byte(`{
		"type": "content_block_start",
		"index": 0,
		"content_block": {"type": "text", "text": ""}
	}`))

	// content_block_delta
	acc.ProcessEvent("content_block_delta", []byte(`{
		"type": "content_block_delta",
		"index": 0,
		"delta": {"type": "text_delta", "text": "Hello, "}
	}`))
	acc.ProcessEvent("content_block_delta", []byte(`{
		"type": "content_block_delta",
		"index": 0,
		"delta": {"type": "text_delta", "text": "world!"}
	}`))

	// content_block_stop
	acc.ProcessEvent("content_block_stop", []byte(`{
		"type": "content_block_stop",
		"index": 0
	}`))

	// message_delta with usage
	acc.ProcessEvent("message_delta", []byte(`{
		"type": "message_delta",
		"delta": {"stop_reason": "end_turn"},
		"usage": {"output_tokens": 10}
	}`))

	// message_stop
	acc.ProcessEvent("message_stop", []byte(`{"type": "message_stop"}`))

	result := acc.Build()
	require.NotNil(t, result)

	var msg map[string]any
	err := json.Unmarshal(result, &msg)
	require.NoError(t, err)

	assert.Equal(t, "msg_123", msg["id"])
	assert.Equal(t, "message", msg["type"])
	assert.Equal(t, "assistant", msg["role"])
	assert.Equal(t, "claude-sonnet-4-20250514", msg["model"])
	assert.Equal(t, "end_turn", msg["stop_reason"])

	content, ok := msg["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 1)

	block := content[0].(map[string]any)
	assert.Equal(t, "text", block["type"])
	assert.Equal(t, "Hello, world!", block["text"])

	usage, ok := msg["usage"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, float64(100), usage["input_tokens"])
	assert.Equal(t, float64(10), usage["output_tokens"])
	assert.Equal(t, float64(50), usage["cache_read_input_tokens"])
}

func TestAnthropicResponseAccumulator_ToolUse(t *testing.T) {
	acc := newAnthropicResponseAccumulator()

	acc.ProcessEvent("message_start", []byte(`{
		"type": "message_start",
		"message": {
			"id": "msg_456",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514"
		}
	}`))

	// tool_use block
	acc.ProcessEvent("content_block_start", []byte(`{
		"type": "content_block_start",
		"index": 0,
		"content_block": {"type": "tool_use", "id": "toolu_01", "name": "get_weather"}
	}`))

	acc.ProcessEvent("content_block_delta", []byte(`{
		"type": "content_block_delta",
		"index": 0,
		"delta": {"type": "input_json_delta", "partial_json": "{\"city\":"}
	}`))
	acc.ProcessEvent("content_block_delta", []byte(`{
		"type": "content_block_delta",
		"index": 0,
		"delta": {"type": "input_json_delta", "partial_json": "\"NYC\"}"}
	}`))

	acc.ProcessEvent("content_block_stop", []byte(`{
		"type": "content_block_stop",
		"index": 0
	}`))

	acc.ProcessEvent("message_delta", []byte(`{
		"type": "message_delta",
		"delta": {"stop_reason": "tool_use"},
		"usage": {"output_tokens": 25}
	}`))

	result := acc.Build()
	require.NotNil(t, result)

	var msg map[string]any
	err := json.Unmarshal(result, &msg)
	require.NoError(t, err)

	assert.Equal(t, "tool_use", msg["stop_reason"])

	content := msg["content"].([]any)
	require.Len(t, content, 1)

	block := content[0].(map[string]any)
	assert.Equal(t, "tool_use", block["type"])
	assert.Equal(t, "toolu_01", block["id"])
	assert.Equal(t, "get_weather", block["name"])

	input, ok := block["input"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "NYC", input["city"])
}

func TestAnthropicResponseAccumulator_EmptyNoMessageStart(t *testing.T) {
	acc := newAnthropicResponseAccumulator()
	result := acc.Build()
	assert.Nil(t, result)
}

func TestAnthropicResponseAccumulator_ThinkingBlock(t *testing.T) {
	acc := newAnthropicResponseAccumulator()

	acc.ProcessEvent("message_start", []byte(`{
		"type": "message_start",
		"message": {
			"id": "msg_789",
			"type": "message",
			"role": "assistant",
			"model": "claude-sonnet-4-20250514"
		}
	}`))

	// thinking block
	acc.ProcessEvent("content_block_start", []byte(`{
		"type": "content_block_start",
		"index": 0,
		"content_block": {"type": "thinking"}
	}`))

	acc.ProcessEvent("content_block_delta", []byte(`{
		"type": "content_block_delta",
		"index": 0,
		"delta": {"type": "thinking_delta", "thinking": "Let me think..."}
	}`))

	acc.ProcessEvent("content_block_stop", []byte(`{
		"type": "content_block_stop",
		"index": 0
	}`))

	// text block
	acc.ProcessEvent("content_block_start", []byte(`{
		"type": "content_block_start",
		"index": 1,
		"content_block": {"type": "text", "text": ""}
	}`))

	acc.ProcessEvent("content_block_delta", []byte(`{
		"type": "content_block_delta",
		"index": 1,
		"delta": {"type": "text_delta", "text": "The answer is 42."}
	}`))

	acc.ProcessEvent("content_block_stop", []byte(`{
		"type": "content_block_stop",
		"index": 1
	}`))

	acc.ProcessEvent("message_delta", []byte(`{
		"type": "message_delta",
		"delta": {"stop_reason": "end_turn"},
		"usage": {"output_tokens": 50}
	}`))

	result := acc.Build()
	require.NotNil(t, result)

	var msg map[string]any
	err := json.Unmarshal(result, &msg)
	require.NoError(t, err)

	content := msg["content"].([]any)
	require.Len(t, content, 2)

	thinking := content[0].(map[string]any)
	assert.Equal(t, "thinking", thinking["type"])
	assert.Equal(t, "Let me think...", thinking["thinking"])

	text := content[1].(map[string]any)
	assert.Equal(t, "text", text["type"])
	assert.Equal(t, "The answer is 42.", text["text"])
}
