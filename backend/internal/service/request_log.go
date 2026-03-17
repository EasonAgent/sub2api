package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var requestLogMu sync.Mutex

type requestLogRecord struct {
	Ts               int64            `json:"ts"`
	UserID           int64            `json:"user_id,omitempty"`
	APIKeyID         int64            `json:"api_key_id,omitempty"`
	RequestBody      json.RawMessage  `json:"request_body"`
	ResponseComplete json.RawMessage  `json:"response_complete,omitempty"`
	ResponseBody     *json.RawMessage `json:"response_body,omitempty"`
}

// requestLogCfgEnabled checks if request logging is enabled in the config.
func requestLogCfgEnabled(cfg *config.Config) bool {
	return cfg != nil && cfg.Gateway.RequestLog.Enabled
}

// isGroupExcludedFromLog checks if the request's group is in the excluded list.
func isGroupExcludedFromLog(c *gin.Context, cfg *config.Config) bool {
	if c == nil || cfg == nil {
		return false
	}
	excluded := cfg.Gateway.RequestLog.ExcludedGroups
	if len(excluded) == 0 {
		return false
	}
	group, ok := c.Request.Context().Value(ctxkey.Group).(*Group)
	if !ok || group == nil {
		return false
	}
	for _, name := range excluded {
		if name == group.Name {
			return true
		}
	}
	return false
}

// writeRequestLogRecord writes a pre-built requestLogRecord to the JSONL file.
func writeRequestLogRecord(c *gin.Context, cfg *config.Config, rec requestLogRecord) {
	line, err := json.Marshal(rec)
	if err != nil {
		logger.FromContext(c.Request.Context()).Warn("request_log: marshal failed", zap.Error(err))
		return
	}
	line = append(line, '\n')

	dir := cfg.Gateway.RequestLog.Dir
	if dir == "" {
		dir = "data/request_logs"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logger.FromContext(c.Request.Context()).Warn("request_log: mkdir failed", zap.Error(err))
		return
	}

	filename := filepath.Join(dir, time.Now().Format("2006-01-02")+".jsonl")

	requestLogMu.Lock()
	defer requestLogMu.Unlock()

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		logger.FromContext(c.Request.Context()).Warn("request_log: open file failed", zap.Error(err))
		return
	}
	defer f.Close()
	_, _ = f.Write(line)
}

// --- OpenAIGatewayService methods (existing) ---

func (s *OpenAIGatewayService) requestLogEnabled() bool {
	return requestLogCfgEnabled(s.cfg)
}

func (s *OpenAIGatewayService) shouldLogRequest(c *gin.Context) bool {
	return s.requestLogEnabled() && !isGroupExcludedFromLog(c, s.cfg)
}

func (s *OpenAIGatewayService) writeRequestLog(c *gin.Context, requestBody, responseData []byte) {
	if len(requestBody) == 0 && len(responseData) == 0 {
		return
	}

	rec := requestLogRecord{
		Ts:          time.Now().Unix(),
		APIKeyID:    getAPIKeyIDFromContext(c),
		RequestBody: json.RawMessage(requestBody),
	}
	if len(responseData) > 0 {
		rec.ResponseComplete = json.RawMessage(responseData)
	}

	writeRequestLogRecord(c, s.cfg, rec)
}

func (s *OpenAIGatewayService) writeRequestLogNonStreaming(c *gin.Context, requestBody, responseBody []byte) {
	if len(requestBody) == 0 && len(responseBody) == 0 {
		return
	}

	resp := json.RawMessage(responseBody)
	rec := requestLogRecord{
		Ts:           time.Now().Unix(),
		APIKeyID:     getAPIKeyIDFromContext(c),
		RequestBody:  json.RawMessage(requestBody),
		ResponseBody: &resp,
	}

	writeRequestLogRecord(c, s.cfg, rec)
}

// --- GatewayService methods (Anthropic /v1/messages) ---

func (s *GatewayService) requestLogEnabled() bool {
	return requestLogCfgEnabled(s.cfg)
}

func (s *GatewayService) shouldLogRequest(c *gin.Context) bool {
	return s.requestLogEnabled() && !isGroupExcludedFromLog(c, s.cfg)
}

// writeAnthropicRequestLog writes a request log entry for Anthropic streaming responses.
// responseData is the accumulated final response JSON (built from SSE events).
func (s *GatewayService) writeAnthropicRequestLog(c *gin.Context, requestBody, responseData []byte) {
	if len(requestBody) == 0 && len(responseData) == 0 {
		return
	}

	rec := requestLogRecord{
		Ts:          time.Now().Unix(),
		APIKeyID:    getAPIKeyIDFromContext(c),
		RequestBody: json.RawMessage(requestBody),
	}
	if len(responseData) > 0 {
		rec.ResponseComplete = json.RawMessage(responseData)
	}

	writeRequestLogRecord(c, s.cfg, rec)
}

// writeAnthropicRequestLogNonStreaming writes a request log entry for non-streaming Anthropic responses.
func (s *GatewayService) writeAnthropicRequestLogNonStreaming(c *gin.Context, requestBody, responseBody []byte) {
	if len(requestBody) == 0 && len(responseBody) == 0 {
		return
	}

	resp := json.RawMessage(responseBody)
	rec := requestLogRecord{
		Ts:           time.Now().Unix(),
		APIKeyID:     getAPIKeyIDFromContext(c),
		RequestBody:  json.RawMessage(requestBody),
		ResponseBody: &resp,
	}

	writeRequestLogRecord(c, s.cfg, rec)
}
