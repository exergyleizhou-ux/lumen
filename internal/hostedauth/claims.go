package hostedauth

import "github.com/golang-jwt/jwt/v5"

const (
	Issuer   = "oasis"
	Audience = "lumen-workbench"
)

type Claims struct {
	UserID      string   `json:"uid"`
	WorkspaceID string   `json:"workspace_id"`
	Permissions []string `json:"permissions"`
	jwt.RegisteredClaims
}

type Identity struct {
	UserID      string
	WorkspaceID string
	SessionID   string
	Permissions []string
}
