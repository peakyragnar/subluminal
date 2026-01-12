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
	SinceCreatedAt string
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

type toolCallQueryBuilder struct {
	clauses    []string
	params     []sqliteParam
	paramIndex map[string]int
}

func newToolCallQueryBuilder() *toolCallQueryBuilder {
	return &toolCallQueryBuilder{
		clauses:    []string{},
		params:     []sqliteParam{},
		paramIndex: make(map[string]int),
	}
}

func (b *toolCallQueryBuilder) addTextParam(name, value string) string {
	return b.addParam(name, value, false)
}

func (b *toolCallQueryBuilder) addIntParam(name string, value int) string {
	return b.addParam(name, fmt.Sprintf("%d", value), true)
}

func (b *toolCallQueryBuilder) addParam(name, value string, numeric bool) string {
	paramName := name
	if !strings.HasPrefix(paramName, ":") && !strings.HasPrefix(paramName, "@") && !strings.HasPrefix(paramName, "$") {
		paramName = ":" + paramName
	}
	if _, ok := b.paramIndex[paramName]; !ok {
		b.paramIndex[paramName] = len(b.params)
		b.params = append(b.params, sqliteParam{
			Name:      paramName,
			Value:     value,
			IsNumeric: numeric,
		})
	}
	return paramName
}

func buildToolCallQuery(columns []string, filters toolCallFilters, orderDesc bool, limit, offset int) sqliteQuery {
	builder := newToolCallQueryBuilder()

	selectCols := strings.Join(columns, ", ")
	query := "SELECT " + selectCols + " FROM tool_calls"

	if filters.RunID != "" {
		param := builder.addTextParam("run_id", filters.RunID)
		builder.clauses = append(builder.clauses, "run_id = "+param)
	}
	if filters.Server != "" {
		param := builder.addTextParam("server_name", filters.Server)
		builder.clauses = append(builder.clauses, "server_name = "+param)
	}
	if filters.Tool != "" {
		param := builder.addTextParam("tool_name", filters.Tool)
		operator := "="
		if isGlobPattern(filters.Tool) {
			operator = "GLOB"
		}
		builder.clauses = append(builder.clauses, "tool_name "+operator+" "+param)
	}
	if filters.Decision != "" {
		param := builder.addTextParam("decision", filters.Decision)
		builder.clauses = append(builder.clauses, "decision = "+param)
	}
	if filters.Status != "" {
		param := builder.addTextParam("status", filters.Status)
		builder.clauses = append(builder.clauses, "status = "+param)
	}
	if filters.SinceCreatedAt != "" {
		param := builder.addTextParam("since_created_at", filters.SinceCreatedAt)
		builder.clauses = append(builder.clauses, "created_at >= "+param)
	}
	if filters.AfterCreatedAt != "" {
		createdParam := builder.addTextParam("after_created_at", filters.AfterCreatedAt)
		if filters.AfterCallID != "" {
			callParam := builder.addTextParam("after_call_id", filters.AfterCallID)
			builder.clauses = append(builder.clauses, fmt.Sprintf("(created_at > %s OR (created_at = %s AND call_id > %s))",
				createdParam,
				createdParam,
				callParam,
			))
		} else {
			builder.clauses = append(builder.clauses, "created_at > "+createdParam)
		}
	}

	if len(builder.clauses) > 0 {
		query += " WHERE " + strings.Join(builder.clauses, " AND ")
	}

	if orderDesc {
		query += " ORDER BY created_at DESC, call_id DESC"
	} else {
		query += " ORDER BY created_at ASC, call_id ASC"
	}

	if limit > 0 {
		limitParam := builder.addIntParam("limit", limit)
		query += " LIMIT " + limitParam
		if offset > 0 {
			offsetParam := builder.addIntParam("offset", offset)
			query += " OFFSET " + offsetParam
		}
	} else if offset > 0 {
		offsetParam := builder.addIntParam("offset", offset)
		query += " LIMIT -1 OFFSET " + offsetParam
	}

	return sqliteQuery{
		SQL:    query,
		Params: builder.params,
	}
}

func isGlobPattern(value string) bool {
	return strings.ContainsAny(value, "*?[")
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
