package service

import "testing"

func TestLogin(t *testing.T) {
	svc := New()

	token, ok := svc.Login("student", "student")
	if !ok {
		t.Fatalf("expected successful login")
	}
	if token != "demo-token" {
		t.Fatalf("unexpected token: %q", token)
	}
}

func TestLoginUnauthorized(t *testing.T) {
	svc := New()

	token, ok := svc.Login("student", "wrong-password")
	if ok {
		t.Fatalf("expected login failure")
	}
	if token != "" {
		t.Fatalf("expected empty token, got %q", token)
	}
}

func TestVerifyHTTP(t *testing.T) {
	svc := New()

	okResult := svc.VerifyHTTP("demo-token")
	if !okResult.Valid {
		t.Fatalf("expected valid token")
	}
	if okResult.Subject != "student" {
		t.Fatalf("unexpected subject: %q", okResult.Subject)
	}

	failResult := svc.VerifyHTTP("bad-token")
	if failResult.Valid {
		t.Fatalf("expected invalid token")
	}
	if failResult.Error != "unauthorized" {
		t.Fatalf("unexpected error: %q", failResult.Error)
	}
}

func TestNewCSRFToken(t *testing.T) {
	svc := New()

	token, err := svc.NewCSRFToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(token) != 64 {
		t.Fatalf("expected 64-char token, got %d", len(token))
	}
}
