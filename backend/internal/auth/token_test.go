package auth

import "testing"

func TestNewOpaqueTokenIsUniqueAndNonEmpty(t *testing.T) {
	a, err := newOpaqueToken()
	if err != nil {
		t.Fatalf("newOpaqueToken: %v", err)
	}
	b, err := newOpaqueToken()
	if err != nil {
		t.Fatalf("newOpaqueToken: %v", err)
	}
	if a == "" || b == "" {
		t.Fatal("token should not be empty")
	}
	if a == b {
		t.Fatal("two generated tokens should not collide")
	}
}

func TestHashTokenIsDeterministicAndDoesNotLeakInput(t *testing.T) {
	token := "example-token"
	h1 := hashToken(token)
	h2 := hashToken(token)
	if h1 != h2 {
		t.Fatal("hashToken should be deterministic for the same input")
	}
	if h1 == token {
		t.Fatal("hashToken output should not equal its input")
	}
	if len(h1) != 64 { // hex-encoded SHA-256
		t.Fatalf("expected 64-char hex digest, got %d chars", len(h1))
	}
}
