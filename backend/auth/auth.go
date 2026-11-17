package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SessionCookieName is the HTTP cookie name used for DockVault authentication.
const SessionCookieName = "dockvault_session"

// Claims holds the signed payload stored inside a session cookie.
type Claims struct {
	Username string `json:"u"`
	Exp      int64  `json:"exp"`
}

// HashPassword hashes a plaintext password with bcrypt.
func HashPassword(password string) (string, error) {
	if len(password) < 6 {
		return "", fmt.Errorf("password must be at least 6 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// VerifyPassword compares a plaintext password against its bcrypt hash.
func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// GenerateSecret returns a random 32-byte hex secret for HMAC signing.
func GenerateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// SignToken creates an HMAC-SHA256 signed session token for username.
func SignToken(secret, username string, ttl time.Duration) (string, error) {
	claims := Claims{
		Username: username,
		Exp:      time.Now().UTC().Add(ttl).Unix(),
	}
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	sig := computeMAC(secret, payload)
	return payload + "." + sig, nil
}

// VerifyToken validates an HMAC-SHA256 signed session token and checks expiry.
func VerifyToken(secret, token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, errors.New("malformed token")
	}
	expectedSig := computeMAC(secret, parts[0])
	if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
		return nil, errors.New("invalid token signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, errors.New("invalid token encoding")
	}
	var claims Claims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, errors.New("invalid token claims")
	}
	if claims.Username == "" || time.Now().UTC().Unix() > claims.Exp {
		return nil, errors.New("session expired")
	}
	return &claims, nil
}

func computeMAC(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
