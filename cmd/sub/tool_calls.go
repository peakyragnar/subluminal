package main

import (
	"fmt"
	"strings"
)

const toolCallFieldCount = 10

var toolCallColumns = []string{
	"call_id",
	"created_at",
	"run_id",
	"server_name",
	"tool_name",
	"decision",
	"status",
	"latency_ms",
	"bytes_in",
	"bytes_out",
}

const toolCallHeader = "ts\trun_id\tserver\ttool\tdecision\tstatus\tlatency_ms\tbytes_in\tbytes_out\tcall_id"

// toolCallFilters scopes query results for tool_calls.
type toolCallFilters struct {
	RunID          string
	Server         string
	Tool           string
	Decision       string
	Status         string
	AfterCreatedAt string
	AfterCallID    string
}

type toolCallRow struct {
	CallID     string
	CreatedAt  string
	RunID      string
	ServerName string
	ToolName   string
	Decision   string
	Status     string
	LatencyMS  string
	BytesIn    string
	BytesOut   string
}

func (r toolCallRow) fingerprint() string {
	return strings.Join([]string{
		r.CallID,
		r.CreatedAt,
		r.RunID,
		r.ServerName,
		r.ToolName,
		r.Decision,
		r.Status,
		r.LatencyMS,
		r.BytesIn,
		r.BytesOut,
	}, "\x1f")
}

func (r toolCallRow) format() string {
	return strings.Join([]string{
		r.CreatedAt,
		r.RunID,
		r.ServerName,
		r.ToolName,
		r.Decision,
		r.Status,
		r.LatencyMS,
		r.BytesIn,
		r.BytesOut,
		r.CallID,
	}, "\t")
}

func buildToolCallQuery(columns []string, filters toolCallFilters, orderDesc bool, limit int) string {
	selectCols := strings.Join(columns, ", ")
	query := "SELECT " + selectCols + " FROM tool_calls"

	clauses := []string{}
	if filters.RunID != "" {
		clauses = append(clauses, "run_id="+sqlText(filters.RunID))
	}
	if filters.Server != "" {
		clauses = append(clauses, "server_name="+sqlText(filters.Server))
	}
	if filters.Tool != "" {
		clauses = append(clauses, "tool_name="+sqlText(filters.Tool))
	}
	if filters.Decision != "" {
		clauses = append(clauses, "decision="+sqlText(filters.Decision))
	}
	if filters.Status != "" {
		clauses = append(clauses, "status="+sqlText(filters.Status))
	}
	if filters.AfterCreatedAt != "" {
		if filters.AfterCallID != "" {
			clauses = append(clauses, fmt.Sprintf("(created_at > %s OR (created_at = %s AND call_id > %s))",
				sqlText(filters.AfterCreatedAt),
				sqlText(filters.AfterCreatedAt),
				sqlText(filters.AfterCallID),
			))
		} else {
			clauses = append(clauses, "created_at > "+sqlText(filters.AfterCreatedAt))
		}
	}

	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}

	if orderDesc {
		query += " ORDER BY created_at DESC, call_id DESC"
	} else {
		query += " ORDER BY created_at ASC, call_id ASC"
	}

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	return query
}

func parseToolCallRows(output string) ([]toolCallRow, error) {
	output = strings.Trim(output, "\r\n")
	if output == "" {
		return nil, nil
	}

	lines := strings.Split(output, "\n")
	rows := make([]toolCallRow, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		fields := strings.Split(line, sqliteSeparator)
		if len(fields) < toolCallFieldCount {
			return nil, fmt.Errorf("expected %d columns, got %d", toolCallFieldCount, len(fields))
		}
		row := toolCallRow{
			CallID:     fields[0],
			CreatedAt:  fields[1],
			RunID:      fields[2],
			ServerName: fields[3],
			ToolName:   fields[4],
			Decision:   fields[5],
			Status:     fields[6],
			LatencyMS:  fields[7],
			BytesIn:    fields[8],
			BytesOut:   fields[9],
		}
		rows = append(rows, row)
	}
	return rows, nil
}
