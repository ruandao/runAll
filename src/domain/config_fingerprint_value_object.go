package domain

import (
	"fmt"
	"strings"
)

type ConfigFingerprint struct {
	Path string
	Hash string
}

func NewConfigFingerprint(path, hash string) (ConfigFingerprint, error) {
	p := strings.TrimSpace(path)
	h := strings.TrimSpace(hash)
	if p == "" {
		return ConfigFingerprint{}, fmt.Errorf("config path is required")
	}
	if h == "" {
		return ConfigFingerprint{}, fmt.Errorf("config hash is required")
	}
	return ConfigFingerprint{
		Path: p,
		Hash: h,
	}, nil
}
