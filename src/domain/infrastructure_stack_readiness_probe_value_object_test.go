package domain

import "testing"

func TestInfrastructureStackReadinessProbe_HTTP(t *testing.T) {
	p := NewHTTPInfrastructureStackReadinessProbe("http://127.0.0.1:18080")
	if p.Kind() != InfrastructureStackProbeHTTP {
		t.Fatalf("kind = %q", p.Kind())
	}
	if p.HTTPURL() != "http://127.0.0.1:18080" {
		t.Fatalf("http url = %q", p.HTTPURL())
	}
	if !p.IsValid() {
		t.Fatal("expected valid probe")
	}
}

func TestInfrastructureStackReadinessProbe_TCP(t *testing.T) {
	p := NewTCPInfrastructureStackReadinessProbe("127.0.0.1:6379")
	if p.Kind() != InfrastructureStackProbeTCP {
		t.Fatalf("kind = %q", p.Kind())
	}
	if p.TCPAddr() != "127.0.0.1:6379" {
		t.Fatalf("tcp addr = %q", p.TCPAddr())
	}
	if !p.IsValid() {
		t.Fatal("expected valid probe")
	}
}
