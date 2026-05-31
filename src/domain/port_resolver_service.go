package domain

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

func ResolveHealthPort(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed == nil || strings.TrimSpace(parsed.Host) == "" || strings.TrimSpace(parsed.Scheme) == "" {
		return ""
	}

	if explicitPort := parsed.Port(); explicitPort != "" {
		if isValidPort(explicitPort) {
			return explicitPort
		}
		return ""
	}

	switch strings.ToLower(parsed.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func ResolveTCPPort(addr string) string {
	return extractPortFromHostAddr(strings.TrimSpace(addr))
}

func ResolveCommandPort(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return ""
	}

	if port := resolveLongPortFlag(fields); port != "" {
		return port
	}
	if port := resolveShortPortFlag(fields); port != "" {
		return port
	}
	if port := resolveEnvPort(fields); port != "" {
		return port
	}
	if port := resolveHostAddrPort(fields); port != "" {
		return port
	}
	return ""
}

func resolveLongPortFlag(fields []string) string {
	for i, token := range fields {
		if token == "--port" && i+1 < len(fields) {
			if port := normalizePortToken(fields[i+1]); isValidPort(port) {
				return port
			}
			continue
		}

		if strings.HasPrefix(token, "--port=") {
			if port := normalizePortToken(strings.TrimPrefix(token, "--port=")); isValidPort(port) {
				return port
			}
		}
	}
	return ""
}

func resolveShortPortFlag(fields []string) string {
	for i, token := range fields {
		if token != "-p" || i+1 >= len(fields) {
			continue
		}
		if port := normalizePortToken(fields[i+1]); isValidPort(port) {
			return port
		}
	}
	return ""
}

func resolveEnvPort(fields []string) string {
	for _, token := range fields {
		if !strings.HasPrefix(token, "PORT=") {
			continue
		}
		if port := normalizePortToken(strings.TrimPrefix(token, "PORT=")); isValidPort(port) {
			return port
		}
	}
	return ""
}

func resolveHostAddrPort(fields []string) string {
	for i, token := range fields {
		lowerToken := strings.ToLower(token)

		if strings.HasPrefix(lowerToken, "--host=") {
			if port := extractPortFromHostAddr(strings.TrimPrefix(token, "--host=")); port != "" {
				return port
			}
			continue
		}
		if strings.HasPrefix(lowerToken, "--addr=") {
			if port := extractPortFromHostAddr(strings.TrimPrefix(token, "--addr=")); port != "" {
				return port
			}
			continue
		}

		if (lowerToken == "--host" || lowerToken == "--addr") && i+1 < len(fields) {
			if port := extractPortFromHostAddr(fields[i+1]); port != "" {
				return port
			}
		}
	}
	return ""
}

func extractPortFromHostAddr(value string) string {
	cleaned := strings.TrimSpace(strings.Trim(value, `"'`))
	if cleaned == "" {
		return ""
	}

	if strings.Contains(cleaned, "://") {
		parsed, err := url.Parse(cleaned)
		if err != nil {
			return ""
		}
		if port := parsed.Port(); isValidPort(port) {
			return port
		}
		return ""
	}

	_, port, err := net.SplitHostPort(cleaned)
	if err != nil {
		return ""
	}
	if isValidPort(port) {
		return port
	}
	return ""
}

func normalizePortToken(token string) string {
	cleaned := strings.TrimSpace(strings.Trim(token, `"'`))
	if cleaned == "" {
		return ""
	}
	for _, ch := range cleaned {
		if ch < '0' || ch > '9' {
			return ""
		}
	}
	return cleaned
}

func isValidPort(port string) bool {
	portValue, err := strconv.Atoi(port)
	if err != nil {
		return false
	}
	return portValue >= 1 && portValue <= 65535
}
