package migrate

import ("testing")

func TestRunnerMigrate(t *testing.T) {
	r := NewRunner()
	r.Register(&Migration{Version: 1, Name: "create_table", Up: func() error { return nil }, Down: func() error { return nil }})
	r.Register(&Migration{Version: 2, Name: "add_column", Up: func() error { return nil }, Down: func() error { return nil }})
	statuses, err := r.Migrate()
	if err != nil { t.Fatal(err) }
	if len(statuses) != 2 { t.Error("status count") }
	for _, s := range statuses {
		if !s.Applied { t.Errorf("v%d should be applied", s.Version) }
	}
	if r.Pending() != 0 { t.Error("no pending") }
}
func TestRunnerRollback(t *testing.T) {
	r := NewRunner()
	r.Register(&Migration{Version: 1, Name: "m1", Up: func() error { return nil }, Down: func() error { return nil }})
	r.Migrate()
	statuses, err := r.Rollback(1)
	if err != nil { t.Fatal(err) }
	if len(statuses) != 1 { t.Error("rollback count") }
	if r.Pending() != 1 { t.Error("should be pending after rollback") }
}
func TestDuplicateVersion(t *testing.T) {
	r := NewRunner()
	if err := r.Register(&Migration{Version: 1, Name: "m1"}); err != nil { t.Fatal(err) }
	if err := r.Register(&Migration{Version: 1, Name: "dup"}); err == nil { t.Error("should reject duplicate") }
}
func TestFormatStatus(t *testing.T) {
	s := FormatStatus([]Status{{Version: 1, Name: "m1", Applied: true}})
	if s == "" { t.Error("format") }
}
