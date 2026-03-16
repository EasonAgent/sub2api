package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RequestLogHandler handles admin request log lookup and stats.
type RequestLogHandler struct {
	readerService *service.RequestLogReaderService
}

// NewRequestLogHandler creates a new RequestLogHandler.
func NewRequestLogHandler(readerService *service.RequestLogReaderService) *RequestLogHandler {
	return &RequestLogHandler{readerService: readerService}
}

// Lookup retrieves a single request log record by response_id.
// GET /api/v1/admin/request-logs/lookup?response_id=resp_xxx
func (h *RequestLogHandler) Lookup(c *gin.Context) {
	responseID := c.Query("response_id")
	if responseID == "" {
		response.BadRequest(c, "response_id query parameter is required")
		return
	}

	rec, err := h.readerService.FindByResponseID(responseID)
	if err != nil {
		response.Error(c, 500, "Failed to search request logs: "+err.Error())
		return
	}
	if rec == nil {
		response.NotFound(c, "Request log not found for response_id: "+responseID)
		return
	}

	response.Success(c, rec)
}

// Stats returns aggregated statistics about stored request logs.
// GET /api/v1/admin/request-logs/stats
func (h *RequestLogHandler) Stats(c *gin.Context) {
	stats, err := h.readerService.GetStats()
	if err != nil {
		response.Error(c, 500, "Failed to get request log stats: "+err.Error())
		return
	}

	response.Success(c, stats)
}
