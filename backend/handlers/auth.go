package handlers

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/muecahit94/dockvault/auth"
	"github.com/muecahit94/dockvault/models"
)

const sessionTTL = 7 * 24 * time.Hour

func (s *Server) bootstrapAuth() {
	if s.cfg.AuthUsername == "" || s.cfg.AuthPassword == "" {
		return
	}
	hash, err := auth.HashPassword(s.cfg.AuthPassword)
	if err != nil {
		return
	}
	var user models.User
	if err := s.db.Where("username = ?", s.cfg.AuthUsername).First(&user).Error; err != nil {
		_ = s.db.Create(&models.User{
			Username:     s.cfg.AuthUsername,
			PasswordHash: hash,
		}).Error
	} else {
		_ = s.db.Model(&user).Update("password_hash", hash).Error
	}
	_ = models.SetSetting(s.db, models.SettingAuthEnabled, "true")
}

func (s *Server) isAuthEnabled() bool {
	return models.GetSetting(s.db, models.SettingAuthEnabled, "false") == "true"
}

func (s *Server) trustedProxyHeader() string {
	if s.cfg.TrustedProxyHeader != "" {
		return s.cfg.TrustedProxyHeader
	}
	return models.GetSetting(s.db, models.SettingTrustedProxyHeader, "")
}

func (s *Server) sessionSecret() string {
	return models.GetSetting(s.db, models.SettingSessionSecret, "fallback-ephemeral-secret")
}

func (s *Server) authenticateRequest(c *fiber.Ctx) (bool, string, string) {
	if headerName := s.trustedProxyHeader(); headerName != "" {
		if user := strings.TrimSpace(c.Get(headerName)); user != "" {
			return true, user, "proxy"
		}
	}
	token := c.Cookies(auth.SessionCookieName)
	if token == "" {
		return false, "", ""
	}
	claims, err := auth.VerifyToken(s.sessionSecret(), token)
	if err != nil {
		return false, "", ""
	}
	return true, claims.Username, "local"
}

// AuthMiddleware enforces session cookie or trusted proxy header auth when enabled.
func (s *Server) AuthMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if !s.isAuthEnabled() {
			return c.Next()
		}
		ok, username, _ := s.authenticateRequest(c)
		if !ok {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "authentication required",
			})
		}
		c.Locals("username", username)
		return c.Next()
	}
}

// GetAuthStatus returns whether authentication is enabled and if the current caller is signed in.
func (s *Server) GetAuthStatus(c *fiber.Ctx) error {
	enabled := s.isAuthEnabled()
	var userCount int64
	s.db.Model(&models.User{}).Count(&userCount)

	authenticated := !enabled
	username := ""
	authMode := "none"
	if enabled {
		ok, u, mode := s.authenticateRequest(c)
		authenticated = ok
		username = u
		authMode = mode
	}

	prefs := s.loadUIPreferences()
	return c.JSON(fiber.Map{
		"enabled":            enabled,
		"authenticated":      authenticated,
		"username":           username,
		"authMode":           authMode,
		"hasUsers":           userCount > 0,
		"trustedProxyHeader": s.trustedProxyHeader(),
		"preferences":        prefs,
	})
}

// SetupAuth initializes built-in authentication when no users or active auth exist yet.
func (s *Server) SetupAuth(c *fiber.Ctx) error {
	var userCount int64
	s.db.Model(&models.User{}).Count(&userCount)
	if s.isAuthEnabled() && userCount > 0 {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"error": "authentication is already configured; sign in to manage users",
		})
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}
	username := strings.TrimSpace(body.Username)
	if username == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "username is required"})
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}

	var existing models.User
	if err := s.db.Where("username = ?", username).First(&existing).Error; err == nil {
		existing.PasswordHash = hash
		if err := s.db.Save(&existing).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
	} else {
		if err := s.db.Create(&models.User{Username: username, PasswordHash: hash}).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}
	}

	if err := models.SetSetting(s.db, models.SettingAuthEnabled, "true"); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
	}
	s.setSessionCookie(c, username)

	return c.JSON(fiber.Map{
		"enabled":       true,
		"authenticated": true,
		"username":      username,
	})
}

// Login validates credentials and issues an HttpOnly signed session cookie.
func (s *Server) Login(c *fiber.Ctx) error {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	var user models.User
	if err := s.db.Where("username = ?", strings.TrimSpace(body.Username)).First(&user).Error; err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid username or password"})
	}
	if !auth.VerifyPassword(user.PasswordHash, body.Password) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{"error": "invalid username or password"})
	}

	s.setSessionCookie(c, user.Username)
	return c.JSON(fiber.Map{
		"authenticated": true,
		"username":      user.Username,
	})
}

// Logout clears the active session cookie.
func (s *Server) Logout(c *fiber.Ctx) error {
	c.Cookie(&fiber.Cookie{
		Name:     auth.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Now().Add(-time.Hour),
		HTTPOnly: true,
		Secure:   isHTTPSRequest(c),
		SameSite: "Lax",
	})
	return c.JSON(fiber.Map{"ok": true})
}

// UpdateAuthSettings updates password, auth enablement, or trusted proxy header while authenticated.
func (s *Server) UpdateAuthSettings(c *fiber.Ctx) error {
	var body struct {
		Enabled            *bool  `json:"enabled"`
		Username           string `json:"username"`
		NewPassword        string `json:"newPassword"`
		TrustedProxyHeader string `json:"trustedProxyHeader"`
	}
	if err := c.BodyParser(&body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "invalid JSON body"})
	}

	if body.NewPassword != "" {
		username := strings.TrimSpace(body.Username)
		if username == "" {
			if localUser, ok := c.Locals("username").(string); ok && localUser != "" {
				username = localUser
			} else {
				var first models.User
				if err := s.db.Order("id asc").First(&first).Error; err == nil {
					username = first.Username
				} else {
					username = "admin"
				}
			}
		}
		hash, err := auth.HashPassword(body.NewPassword)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
		}
		var user models.User
		if err := s.db.Where("username = ?", username).First(&user).Error; err == nil {
			s.db.Model(&user).Update("password_hash", hash)
		} else {
			s.db.Create(&models.User{Username: username, PasswordHash: hash})
		}
	}

	if body.Enabled != nil {
		if *body.Enabled {
			var count int64
			s.db.Model(&models.User{}).Count(&count)
			if count == 0 && strings.TrimSpace(body.TrustedProxyHeader) == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"error": "create an admin user password or configure a trusted proxy header before enabling auth",
				})
			}
			_ = models.SetSetting(s.db, models.SettingAuthEnabled, "true")
			if localUser, ok := c.Locals("username").(string); ok && localUser != "" {
				s.setSessionCookie(c, localUser)
			}
		} else {
			_ = models.SetSetting(s.db, models.SettingAuthEnabled, "false")
		}
	}

	if s.cfg.TrustedProxyHeader == "" {
		_ = models.SetSetting(s.db, models.SettingTrustedProxyHeader, strings.TrimSpace(body.TrustedProxyHeader))
	}

	return s.GetAuthStatus(c)
}

func isHTTPSRequest(c *fiber.Ctx) bool {
	return c.Protocol() == "https" || strings.EqualFold(strings.TrimSpace(c.Get("X-Forwarded-Proto")), "https")
}

func (s *Server) setSessionCookie(c *fiber.Ctx, username string) {
	token, err := auth.SignToken(s.sessionSecret(), username, sessionTTL)
	if err != nil {
		return
	}
	c.Cookie(&fiber.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().UTC().Add(sessionTTL),
		HTTPOnly: true,
		Secure:   isHTTPSRequest(c),
		SameSite: "Lax",
	})
}
