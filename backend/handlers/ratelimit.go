package handlers

import (
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

// Login brute-force protection: failed attempts per client IP + username within a fixed window.
const (
	loginMaxFailedAttempts = 10
	loginLockoutWindow     = 15 * time.Minute
)

func (s *Server) loginRateLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:                    loginMaxFailedAttempts,
		Expiration:             loginLockoutWindow,
		SkipSuccessfulRequests: true,
		// Including the username keeps one attacker from locking out every account when many
		// users share an address (e.g. an unconfigured reverse proxy).
		KeyGenerator: func(c *fiber.Ctx) string {
			return s.clientIP(c) + "|" + loginUsernameKey(c.Body())
		},
		LimitReached: func(c *fiber.Ctx) error {
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "too many failed login attempts; please try again later",
			})
		},
	})
}

func loginUsernameKey(body []byte) string {
	var payload struct {
		Username string `json:"username"`
	}
	_ = json.Unmarshal(body, &payload)
	name := strings.ToLower(strings.TrimSpace(payload.Username))
	if len(name) > 100 {
		name = name[:100]
	}
	return name
}

// clientIP returns the caller address, honoring X-Forwarded-For only from trusted proxies.
func (s *Server) clientIP(c *fiber.Ctx) string {
	return resolveClientIP(c.Context().RemoteIP(), c.Get(fiber.HeaderXForwardedFor), s.trustedProxies)
}

// resolveClientIP walks X-Forwarded-For from the right, skipping trusted proxy hops, and returns
// the first untrusted address. The leftmost entries are client-controlled and never trusted blindly.
func resolveClientIP(remote net.IP, forwardedFor string, trusted []*net.IPNet) string {
	if remote == nil {
		return ""
	}
	if !ipTrusted(remote, trusted) || strings.TrimSpace(forwardedFor) == "" {
		return remote.String()
	}
	hops := strings.Split(forwardedFor, ",")
	resolved := remote
	for i := len(hops) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(hops[i]))
		if ip == nil {
			break
		}
		resolved = ip
		if !ipTrusted(ip, trusted) {
			break
		}
	}
	return resolved.String()
}

func ipTrusted(ip net.IP, trusted []*net.IPNet) bool {
	for _, n := range trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// parseTrustedProxies parses comma/space-separated IPs or CIDRs, ignoring invalid entries.
func parseTrustedProxies(raw string) []*net.IPNet {
	var out []*net.IPNet
	for _, part := range strings.Fields(strings.ReplaceAll(raw, ",", " ")) {
		if !strings.Contains(part, "/") {
			if ip := net.ParseIP(part); ip != nil {
				bits := 128
				if ip.To4() != nil {
					bits = 32
				}
				part = part + "/" + strconv.Itoa(bits)
			}
		}
		if _, n, err := net.ParseCIDR(part); err == nil {
			out = append(out, n)
		}
	}
	return out
}
