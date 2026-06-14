// Package diff describes file-level changes for preview and checkpoint snapshots.
package diff

// Change describes the effect a writer tool would have on one file.
type Change struct {
	Path    string `json:"path"`
	Before  string `json:"before,omitempty"`  // pre-edit content (checkpoint)
	After   string `json:"after,omitempty"`   // post-edit content (preview)
	Binary  bool   `json:"binary,omitempty"`  // true when the file is binary (no text diff)
	Removed bool   `json:"removed,omitempty"` // file would be deleted
	New     bool   `json:"new,omitempty"`     // file would be created
}
