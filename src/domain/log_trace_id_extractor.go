package domain

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	traceBracketPattern = regexp.MustCompile(`\[trace_id=([^\]]+)\]`)
	traceJSONPattern    = regexp.MustCompile(`"trace_id"\s*:\s*"([^"]+)"`)
)

// ExtractTraceIDFromLogMessage finds trace_id in JSON or legacy bracket log lines.
func ExtractTraceIDFromLogMessage(message string) string {
	msg := strings.TrimSpace(message)
	if msg == "" {
		return ""
	}

	var payload map[string]any
	if strings.HasPrefix(msg, "{") && json.Unmarshal([]byte(msg), &payload) == nil {
		for _, key := range []string{"trace_id", "traceId"} {
			if raw, ok := payload[key]; ok {
				if tid, err := ParseTraceId(strings.TrimSpace(anyString(raw))); err == nil {
					return tid.String()
				}
			}
		}
	}

	if m := traceJSONPattern.FindStringSubmatch(msg); len(m) == 2 {
		if tid, err := ParseTraceId(strings.TrimSpace(m[1])); err == nil {
			return tid.String()
		}
	}
	if m := traceBracketPattern.FindStringSubmatch(msg); len(m) == 2 {
		if tid, err := ParseTraceId(strings.TrimSpace(m[1])); err == nil {
			return tid.String()
		}
	}
	return ""
}

func anyString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
