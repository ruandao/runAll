package domain

import (
	"testing"
	"time"
)

func TestCascadeStepActorResolver_Resolve(t *testing.T) {
	resolver := NewCascadeStepActorResolver()
	ownership, err := NewServiceOwnership(
		"vue-frontend",
		"runall-bootstrap",
		1234,
		"hash",
		"http://127.0.0.1:4000/health",
		time.Now(),
	)
	if err != nil {
		t.Fatalf("NewServiceOwnership: %v", err)
	}

	tests := []struct {
		name             string
		initiating       string
		ownership        ServiceOwnership
		hasOwnership     bool
		want             string
	}{
		{
			name:         "initiating actor is owner",
			initiating:   "runall-bootstrap",
			ownership:    ownership,
			hasOwnership: true,
			want:         "runall-bootstrap",
		},
		{
			name:         "delegate to registered owner",
			initiating:   "ui-session",
			ownership:    ownership,
			hasOwnership: true,
			want:         "runall-bootstrap",
		},
		{
			name:         "no ownership uses initiating actor",
			initiating:   "ui-session",
			hasOwnership: false,
			want:         "ui-session",
		},
		{
			name:         "empty initiating actor stays empty",
			initiating:   "",
			ownership:    ownership,
			hasOwnership: true,
			want:         "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolver.Resolve(tt.initiating, tt.ownership, tt.hasOwnership)
			if got != tt.want {
				t.Fatalf("Resolve() = %q, want %q", got, tt.want)
			}
		})
	}
}
