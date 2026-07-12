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
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("workbench JWT secret required")
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
	if err != nil || !token.Valid || claims.Subject == "" || claims.ID == "" || claims.UserID == "" || claims.UserID != claims.Subject || claims.WorkspaceID == "" || claims.IssuedAt == nil || claims.NotBefore == nil || len(claims.Permissions) == 0 {
		return Identity{}, ErrUnauthorized
	}
	return Identity{UserID: claims.UserID, WorkspaceID: claims.WorkspaceID, SessionID: claims.ID, Permissions: append([]string(nil), claims.Permissions...)}, nil
}
