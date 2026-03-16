package admin

import (
	"strconv"

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

// List returns lightweight record summaries for a given date with pagination.
// GET /api/v1/admin/request-logs/list?date=2026-03-13&offset=0&limit=20
func (h *RequestLogHandler) List(c *gin.Context) {
	date := c.Query("date")
	if date == "" {
		response.BadRequest(c, "date query parameter is required (YYYY-MM-DD)")
		return
	}

	offset := 0
	limit := 20
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}

	result, err := h.readerService.ListByDate(date, offset, limit)
	if err != nil {
		response.Error(c, 500, "Failed to list request logs: "+err.Error())
		return
	}

	response.Success(c, result)
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
