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
