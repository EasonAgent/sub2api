package service

import "encoding/json"

// anthropicResponseAccumulator accumulates Anthropic SSE events to reconstruct
// the final message object for request logging. It captures:
//   - message_start: message envelope (id, type, role, model)
//   - content_block_start/delta/stop: content blocks (text, tool_use, thinking)
//   - message_delta: stop_reason, stop_sequence, usage
type anthropicResponseAccumulator struct {
	messageID   string
	messageType string
	messageRole string
	model       string
	stopReason  string
	usage       map[string]any
	content     []anthropicContentBlock
	curBlockIdx int
}

type anthropicContentBlock struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
	Text  string `json:"text,omitempty"`
	// thinking block fields
	Thinking string `json:"thinking,omitempty"`
}

func newAnthropicResponseAccumulator() *anthropicResponseAccumulator {
	return &anthropicResponseAccumulator{
		curBlockIdx: -1,
	}
}

// ProcessEvent processes a parsed SSE data JSON line. eventType is the SSE "event:" value.
func (a *anthropicResponseAccumulator) ProcessEvent(eventType string, data []byte) {
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return
	}

	typ, _ := event["type"].(string)
	if typ == "" {
		typ = eventType
	}

	switch typ {
	case "message_start":
		msg, _ := event["message"].(map[string]any)
		if msg == nil {
			return
		}
		a.messageID, _ = msg["id"].(string)
		a.messageType, _ = msg["type"].(string)
		a.messageRole, _ = msg["role"].(string)
		a.model, _ = msg["model"].(string)
		if u, ok := msg["usage"].(map[string]any); ok {
			a.usage = u
		}

	case "content_block_start":
		cb, _ := event["content_block"].(map[string]any)
		if cb == nil {
			return
		}
		block := anthropicContentBlock{}
		block.Type, _ = cb["type"].(string)
		block.ID, _ = cb["id"].(string)
		block.Name, _ = cb["name"].(string)
		if t, ok := cb["text"].(string); ok {
			block.Text = t
		}
		a.content = append(a.content, block)
		a.curBlockIdx = len(a.content) - 1

	case "content_block_delta":
		if a.curBlockIdx < 0 || a.curBlockIdx >= len(a.content) {
			return
		}
		delta, _ := event["delta"].(map[string]any)
		if delta == nil {
			return
		}
		deltaType, _ := delta["type"].(string)
		switch deltaType {
		case "text_delta":
			if t, ok := delta["text"].(string); ok {
				a.content[a.curBlockIdx].Text += t
			}
		case "input_json_delta":
			if t, ok := delta["partial_json"].(string); ok {
				a.content[a.curBlockIdx].Text += t // accumulate raw JSON string
			}
		case "thinking_delta":
			if t, ok := delta["thinking"].(string); ok {
				a.content[a.curBlockIdx].Thinking += t
			}
		}

	case "content_block_stop":
		// If the current block is tool_use, try to parse accumulated input JSON
		if a.curBlockIdx >= 0 && a.curBlockIdx < len(a.content) {
			block := &a.content[a.curBlockIdx]
			if block.Type == "tool_use" && block.Text != "" {
				var parsed any
				if err := json.Unmarshal([]byte(block.Text), &parsed); err == nil {
					block.Input = parsed
				} else {
					block.Input = block.Text
				}
				block.Text = ""
			}
		}
		// Don't reset curBlockIdx; next content_block_start will update it.

	case "message_delta":
		delta, _ := event["delta"].(map[string]any)
		if delta != nil {
			if sr, ok := delta["stop_reason"].(string); ok {
				a.stopReason = sr
			}
		}
		if u, ok := event["usage"].(map[string]any); ok {
			// Merge usage from message_delta (has output_tokens) into accumulated usage
			if a.usage == nil {
				a.usage = u
			} else {
				for k, v := range u {
					a.usage[k] = v
				}
			}
		}
	}
}

// Build serializes the accumulated response as JSON.
// Returns nil if no message_start was received.
func (a *anthropicResponseAccumulator) Build() []byte {
	if a.messageID == "" {
		return nil
	}

	// Build content array
	content := make([]map[string]any, 0, len(a.content))
	for _, block := range a.content {
		cb := map[string]any{"type": block.Type}
		if block.ID != "" {
			cb["id"] = block.ID
		}
		switch block.Type {
		case "text":
			cb["text"] = block.Text
		case "tool_use":
			if block.Name != "" {
				cb["name"] = block.Name
			}
			if block.Input != nil {
				cb["input"] = block.Input
			}
		case "thinking":
			cb["thinking"] = block.Thinking
		default:
			if block.Text != "" {
				cb["text"] = block.Text
			}
		}
		content = append(content, cb)
	}

	msg := map[string]any{
		"id":      a.messageID,
		"type":    a.messageType,
		"role":    a.messageRole,
		"model":   a.model,
		"content": content,
	}
	if a.stopReason != "" {
		msg["stop_reason"] = a.stopReason
	}
	if a.usage != nil {
		msg["usage"] = a.usage
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil
	}
	return data
}
