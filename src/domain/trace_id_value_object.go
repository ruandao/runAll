package domain

import (
	"fmt"
	"regexp"
	"strings"
)

var traceIDPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,256}$`)

// TraceId identifies a request correlation id aligned with saas-backend core.http_trace.
type TraceId struct {
	value string
}

func ParseTraceId(raw string) (TraceId, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return TraceId{}, fmt.Errorf("trace id is required")
	}
	if len(s) > 256 {
		return TraceId{}, fmt.Errorf("trace id exceeds max length 256")
	}
	if !traceIDPattern.MatchString(s) {
		return TraceId{}, fmt.Errorf("trace id has invalid format")
	}
	return TraceId{value: s}, nil
}

func (t TraceId) String() string {
	return t.value
}

func (t TraceId) IsZero() bool {
	return t.value == ""
}
