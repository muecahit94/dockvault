package auth

import (
	"testing"
	"time"
)

func TestPasswordAndTokenLifecycle(t *testing.T) {
	if _, err := HashPassword("123"); err == nil {
		t.Fatalf("expected short password error")
	}
	hash, err := HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !VerifyPassword(hash, "correct-horse-battery") || VerifyPassword(hash, "wrong") {
		t.Fatalf("password verification mismatch")
	}

	secret, err := GenerateSecret()
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	token, err := SignToken(secret, "admin", time.Hour)
	if err != nil {
		t.Fatalf("SignToken: %v", err)
	}
	claims, err := VerifyToken(secret, token)
	if err != nil || claims.Username != "admin" {
		t.Fatalf("VerifyToken failed: claims=%+v err=%v", claims, err)
	}

	if _, err := VerifyToken(secret, token+"tampered"); err == nil {
		t.Fatalf("expected tampered token rejection")
	}

	expired, _ := SignToken(secret, "admin", -time.Minute)
	if _, err := VerifyToken(secret, expired); err == nil {
		t.Fatalf("expected expired token rejection")
	}
}
