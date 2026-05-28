package domain

import (
	"fmt"
	"net/url"
	"strings"
)

// GrafanaTraceLink builds a deep link to the trace dashboard Explore view.
func GrafanaTraceLink(baseURL, dashboardUID, traceID string) (string, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return "", fmt.Errorf("grafana base url is required")
	}
	uid := strings.TrimSpace(dashboardUID)
	if uid == "" {
		uid = "trace-log-journey"
	}
	tid, err := ParseTraceId(traceID)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return "", fmt.Errorf("parse grafana url: %w", err)
	}
	u.Path = fmt.Sprintf("/d/%s/trace-log-journey", uid)
	q := u.Query()
	q.Set("var-trace_id", tid.String())
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// GrafanaLokiExploreLink opens Grafana Explore with the provisioned Loki datasource (uid=loki).
func GrafanaLokiExploreLink(baseURL string) (string, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return "", fmt.Errorf("grafana base url is required")
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return "", fmt.Errorf("parse grafana url: %w", err)
	}
	u.Path = "/explore"
	left := `{"datasource":"loki","queries":[{"refId":"A","expr":"{job=\"runall\"}"}]}`
	q := u.Query()
	q.Set("orgId", "1")
	q.Set("left", left)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
