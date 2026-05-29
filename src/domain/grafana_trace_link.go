package domain

import (
	"fmt"
	"net/url"
	"strings"
)

// GrafanaTraceLink builds a deep link to the distributed trace dashboard.
func GrafanaTraceLink(baseURL, dashboardUID, traceID string) (string, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return "", fmt.Errorf("grafana base url is required")
	}
	uid := strings.TrimSpace(dashboardUID)
	if uid == "" {
		uid = "distributed-trace-view"
	}
	tid, err := ParseTraceId(traceID)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return "", fmt.Errorf("parse grafana url: %w", err)
	}
	u.Path = fmt.Sprintf("/d/%s", uid)
	q := u.Query()
	q.Set("var-trace_id", tid.String())
	q.Set("var-tempo_trace_id", OtelTraceIDHex(tid.String()))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// GrafanaTempoExploreLink opens Grafana Explore with Tempo traceId search.
func GrafanaTempoExploreLink(baseURL, traceID string) (string, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return "", fmt.Errorf("grafana base url is required")
	}
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return "", fmt.Errorf("parse grafana url: %w", err)
	}
	u.Path = "/explore"
	left := `{"datasource":"tempo","queries":[{"refId":"A","queryType":"traceId","query":""}]}`
	if strings.TrimSpace(traceID) != "" {
		tid, err := ParseTraceId(traceID)
		if err != nil {
			return "", err
		}
		left = fmt.Sprintf(
			`{"datasource":"tempo","queries":[{"refId":"A","queryType":"traceId","query":%q}]}`,
			OtelTraceIDHex(tid.String()),
		)
	}
	q := u.Query()
	q.Set("orgId", "1")
	q.Set("left", left)
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
