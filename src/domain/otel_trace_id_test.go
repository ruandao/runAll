package domain

import "testing"

func TestOtelTraceIDHex_uuid(t *testing.T) {
	got := OtelTraceIDHex("1cd1a1cc-e64d-4325-8b31-caabdd8aa74d")
	want := "1cd1a1cce64d43258b31caabdd8aa74d"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestOtelTraceIDHex_legacyDeterministic(t *testing.T) {
	a := OtelTraceIDHex("trace-link-test1234")
	b := OtelTraceIDHex("trace-link-test1234")
	if a != b || len(a) != 32 {
		t.Fatalf("unexpected %q", a)
	}
}
