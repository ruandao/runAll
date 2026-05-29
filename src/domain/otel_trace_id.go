package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var hex32 = regexp.MustCompile(`(?i)^[0-9a-f]{32}$`)

// OtelTraceIDHex maps X-Trace-Id to Tempo-compatible 32-char lowercase hex.
func OtelTraceIDHex(external string) string {
	raw := strings.TrimSpace(external)
	compact := strings.ReplaceAll(raw, "-", "")
	if hex32.MatchString(compact) {
		return strings.ToLower(compact)
	}
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:16])
}
