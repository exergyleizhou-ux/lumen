package hostedauth

import (
	"github.com/golang-jwt/jwt/v5"
	"testing"
	"time"
)

func signed(t *testing.T, secret string, method jwt.SigningMethod, mutate func(*Claims)) string {
	t.Helper()
	now := time.Now()
	c := &Claims{UserID: "user-1", WorkspaceID: "workspace-1", Permissions: []string{"code:run"}, RegisteredClaims: jwt.RegisteredClaims{Issuer: Issuer, Audience: jwt.ClaimStrings{Audience}, Subject: "user-1", ID: "session-1", IssuedAt: jwt.NewNumericDate(now), NotBefore: jwt.NewNumericDate(now.Add(-time.Second)), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))}}
	if mutate != nil {
		mutate(c)
	}
	raw, err := jwt.NewWithClaims(method, c).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestVerifierRejectsInvalidTokensWithoutLeakage(t *testing.T) {
	v, _ := NewVerifier("correct-secret")
	cases := map[string]string{
		"bad signature":       signed(t, "wrong-secret", jwt.SigningMethodHS256, nil),
		"bad algorithm":       signed(t, "correct-secret", jwt.SigningMethodHS384, nil),
		"bad audience":        signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.Audience = jwt.ClaimStrings{"other"} }),
		"expired":             signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute)) }),
		"future nbf":          signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.NotBefore = jwt.NewNumericDate(time.Now().Add(time.Minute)) }),
		"missing issuer":      signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.Issuer = "" }),
		"wrong issuer":        signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.Issuer = "other" }),
		"missing audience":    signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.Audience = nil }),
		"missing expiry":      signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.ExpiresAt = nil }),
		"missing subject":     signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.Subject = "" }),
		"missing jti":         signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.ID = "" }),
		"missing uid":         signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.UserID = "" }),
		"mismatched uid":      signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.UserID = "other" }),
		"missing workspace":   signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.WorkspaceID = "" }),
		"missing iat":         signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.IssuedAt = nil }),
		"future iat":          signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.IssuedAt = jwt.NewNumericDate(time.Now().Add(time.Minute)) }),
		"missing nbf":         signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.NotBefore = nil }),
		"missing permissions": signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.Permissions = nil }),
		"workspace traversal": signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.WorkspaceID = "../victim/ws" }),
		"workspace slash":     signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.WorkspaceID = "victim/ws" }),
		"workspace dot":       signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.WorkspaceID = "." }),
		"workspace encoded":   signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.WorkspaceID = "%2e%2e%2fvictim" }),
		"user traversal":      signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.UserID = "../victim"; c.Subject = c.UserID }),
		"user backslash":      signed(t, "correct-secret", jwt.SigningMethodHS256, func(c *Claims) { c.UserID = `victim\user`; c.Subject = c.UserID }),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := v.Verify(raw); err != ErrUnauthorized {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestVerifierReturnsIdentity(t *testing.T) {
	v, _ := NewVerifier("secret")
	got, err := v.Verify(signed(t, "secret", jwt.SigningMethodHS256, nil))
	if err != nil || got.UserID != "user-1" || got.WorkspaceID != "workspace-1" || got.SessionID != "session-1" {
		t.Fatalf("got %#v, %v", got, err)
	}
}
func TestVerifierRequiresSecret(t *testing.T) {
	if _, err := NewVerifier(" "); err == nil {
		t.Fatal("expected error")
	}
}
