package project

import "time"

// Project is a science lab workspace.
type Project struct {
	ID            string    `json:"id"`
	Slug          string    `json:"slug"`
	Title         string    `json:"title"`
	Template      string    `json:"template,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	ActiveSession string    `json:"active_session,omitempty"`
	WorkspaceRel  string    `json:"workspace_rel"` // always "workspace"
}

// Session is a conversation within a project.
type Session struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
