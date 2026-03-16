package service

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// RequestLogReaderService reads and queries JSONL request log files.
type RequestLogReaderService struct {
	cfg *config.Config
}

// NewRequestLogReaderService creates a new RequestLogReaderService.
func NewRequestLogReaderService(cfg *config.Config) *RequestLogReaderService {
	return &RequestLogReaderService{cfg: cfg}
}

// RequestLogRecord represents a single JSONL log entry.
type RequestLogRecord struct {
	Ts               int64            `json:"ts"`
	UserID           int64            `json:"user_id,omitempty"`
	APIKeyID         int64            `json:"api_key_id,omitempty"`
	RequestBody      json.RawMessage  `json:"request_body"`
	ResponseComplete json.RawMessage  `json:"response_complete,omitempty"`
	ResponseBody     *json.RawMessage `json:"response_body,omitempty"`
}

// RequestLogStats holds aggregated statistics about stored logs.
type RequestLogStats struct {
	TotalRecords int64              `json:"total_records"`
	DateRange    *DateRange         `json:"date_range,omitempty"`
	PerDate      []DateRecordCount  `json:"per_date"`
	DiskUsage    int64              `json:"disk_usage_bytes"`
}

// DateRange represents the earliest and latest log dates.
type DateRange struct {
	Earliest string `json:"earliest"`
	Latest   string `json:"latest"`
}

// DateRecordCount holds the record count and file size for a single date.
type DateRecordCount struct {
	Date      string `json:"date"`
	Records   int64  `json:"records"`
	SizeBytes int64  `json:"size_bytes"`
}

func (s *RequestLogReaderService) logDir() string {
	dir := s.cfg.Gateway.RequestLog.Dir
	if dir == "" {
		dir = "data/request_logs"
	}
	return dir
}

// FindByResponseID scans JSONL files for a record whose response_complete.response.id matches the given ID.
func (s *RequestLogReaderService) FindByResponseID(responseID string) (*RequestLogRecord, error) {
	dir := s.logDir()
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("glob log files: %w", err)
	}
	// Search newest files first (more likely to find recent records)
	sort.Sort(sort.Reverse(sort.StringSlice(files)))

	needle := fmt.Sprintf(`"id":"%s"`, responseID)

	for _, file := range files {
		rec, err := searchFileForResponseID(file, needle, responseID)
		if err != nil {
			continue // skip corrupt files
		}
		if rec != nil {
			return rec, nil
		}
	}
	return nil, nil
}

func searchFileForResponseID(filePath, needle, responseID string) (*RequestLogRecord, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // up to 10MB per line

	for scanner.Scan() {
		line := scanner.Bytes()
		// Fast string check before JSON parsing
		if !strings.Contains(string(line), needle) {
			continue
		}
		var rec RequestLogRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if matchesResponseID(rec.ResponseComplete, responseID) {
			return &rec, nil
		}
	}
	return nil, scanner.Err()
}

func matchesResponseID(data json.RawMessage, responseID string) bool {
	if len(data) == 0 {
		return false
	}
	// response_complete can be either the event wrapper or the response object directly.
	// Try: .response.id first (event wrapper from SSE response.completed)
	var wrapper struct {
		Response struct {
			ID string `json:"id"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &wrapper); err == nil && wrapper.Response.ID == responseID {
		return true
	}
	// Try: .id directly (raw response object)
	var direct struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &direct); err == nil && direct.ID == responseID {
		return true
	}
	return false
}

// GetStats returns aggregated statistics for all JSONL log files.
func (s *RequestLogReaderService) GetStats() (*RequestLogStats, error) {
	dir := s.logDir()
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("glob log files: %w", err)
	}
	sort.Strings(files)

	stats := &RequestLogStats{
		PerDate: make([]DateRecordCount, 0, len(files)),
	}

	for _, file := range files {
		date := strings.TrimSuffix(filepath.Base(file), ".jsonl")
		info, err := os.Stat(file)
		if err != nil {
			continue
		}

		count := countLines(file)
		stats.TotalRecords += count
		stats.DiskUsage += info.Size()
		stats.PerDate = append(stats.PerDate, DateRecordCount{
			Date:      date,
			Records:   count,
			SizeBytes: info.Size(),
		})
	}

	if len(stats.PerDate) > 0 {
		stats.DateRange = &DateRange{
			Earliest: stats.PerDate[0].Date,
			Latest:   stats.PerDate[len(stats.PerDate)-1].Date,
		}
	}

	return stats, nil
}

// RequestLogSummary is a lightweight record summary for list views.
type RequestLogSummary struct {
	ResponseID string `json:"response_id"`
	Ts         int64  `json:"ts"`
	APIKeyID   int64  `json:"api_key_id"`
	Model      string `json:"model"`
}

// RequestLogListResult holds paginated list results.
type RequestLogListResult struct {
	Records []RequestLogSummary `json:"records"`
	Total   int64               `json:"total"`
	Offset  int                 `json:"offset"`
	Limit   int                 `json:"limit"`
}

// ListByDate returns lightweight record summaries for a given date with pagination.
func (s *RequestLogReaderService) ListByDate(date string, offset, limit int) (*RequestLogListResult, error) {
	dir := s.logDir()
	filePath := filepath.Join(dir, date+".jsonl")

	f, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &RequestLogListResult{Records: []RequestLogSummary{}, Offset: offset, Limit: limit}, nil
		}
		return nil, fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var all []RequestLogSummary
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		summary := extractSummary(line)
		all = append(all, summary)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan log file: %w", err)
	}

	total := int64(len(all))

	// Reverse to show newest first
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}

	// Apply pagination
	start := offset
	if start > len(all) {
		start = len(all)
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}

	return &RequestLogListResult{
		Records: all[start:end],
		Total:   total,
		Offset:  offset,
		Limit:   limit,
	}, nil
}

// extractSummary extracts lightweight fields from a JSONL line without full parsing.
func extractSummary(line []byte) RequestLogSummary {
	var raw struct {
		Ts               int64           `json:"ts"`
		APIKeyID         int64           `json:"api_key_id"`
		RequestBody      json.RawMessage `json:"request_body"`
		ResponseComplete json.RawMessage `json:"response_complete"`
	}
	_ = json.Unmarshal(line, &raw)

	summary := RequestLogSummary{
		Ts:       raw.Ts,
		APIKeyID: raw.APIKeyID,
	}

	// Extract model from request_body.model
	if len(raw.RequestBody) > 0 {
		var rb struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(raw.RequestBody, &rb)
		summary.Model = rb.Model
	}

	// Extract response_id from response_complete.response.id
	if len(raw.ResponseComplete) > 0 {
		var wrapper struct {
			Response struct {
				ID string `json:"id"`
			} `json:"response"`
		}
		if json.Unmarshal(raw.ResponseComplete, &wrapper) == nil && wrapper.Response.ID != "" {
			summary.ResponseID = wrapper.Response.ID
		} else {
			var direct struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(raw.ResponseComplete, &direct) == nil {
				summary.ResponseID = direct.ID
			}
		}
	}

	return summary
}

func countLines(filePath string) int64 {
	f, err := os.Open(filePath)
	if err != nil {
		return 0
	}
	defer f.Close()

	var count int64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			count++
		}
	}
	return count
}
