package hostedauth

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var ErrUnauthorized = errors.New("unauthorized")

type Verifier struct {
	secret []byte
	now    func() time.Time
}

func NewVerifier(secret string) (*Verifier, error) {
	secret = strings.TrimSpace(secret)
	if len(secret) < 32 {
		return nil, errors.New("workbench JWT secret must be at least 32 bytes")
	}
	return &Verifier{secret: []byte(secret), now: time.Now}, nil
}

func (v *Verifier) Verify(raw string) (Identity, error) {
	claims := new(Claims)
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, ErrUnauthorized
		}
		return v.secret, nil
	}, jwt.WithIssuer(Issuer), jwt.WithAudience(Audience), jwt.WithExpirationRequired(), jwt.WithIssuedAt(), jwt.WithValidMethods([]string{"HS256"}), jwt.WithTimeFunc(v.now))
	if err != nil || !token.Valid || claims.Subject == "" || claims.ID == "" || !safeIdentityComponent(claims.UserID) || claims.UserID != claims.Subject || !safeIdentityComponent(claims.WorkspaceID) || claims.IssuedAt == nil || claims.NotBefore == nil || len(claims.Permissions) == 0 {
		return Identity{}, ErrUnauthorized
	}
	return Identity{UserID: claims.UserID, WorkspaceID: claims.WorkspaceID, SessionID: claims.ID, Permissions: append([]string(nil), claims.Permissions...)}, nil
}

// safeIdentityComponent permits UUIDs and ordinary local slugs, but never a
// value that could be interpreted as a path (including percent-encoded paths).
func safeIdentityComponent(value string) bool {
	if value == "" || value == "." || value == ".." || len(value) > 128 {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
