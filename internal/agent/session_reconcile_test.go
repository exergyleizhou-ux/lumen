package agent

import (
	"encoding/json"
	"testing"

	"lumen/internal/provider"
)

func TestReconcileLoad(t *testing.T) {
	user := provider.Message{Role: provider.RoleUser, Content: "hi"}
	rowHi, _ := marshalMsg(user)
	rowBye, _ := marshalMsg(provider.Message{Role: provider.RoleAssistant, Content: "bye"})

	tests := []struct {
		name        string
		state       jsonlFileState
		jsonl       []provider.Message
		sqlite      [][]byte
		wantLen     int
		wantFirst   string
		wantClear   bool
		wantReplace bool
		wantMigrate bool
	}{
		{
			name:    "unreadable_no_sqlite_empty_session",
			state:   jsonlUnreadable,
			wantLen: 0,
		},
		{
			name:      "unreadable_with_sqlite_fallback",
			state:     jsonlUnreadable,
			sqlite:    [][]byte{rowHi},
			wantLen:   1,
			wantFirst: "hi",
		},
		{
			name:      "present_empty_clears_sqlite",
			state:     jsonlPresentEmpty,
			sqlite:    [][]byte{rowHi},
			wantLen:   0,
			wantClear: true,
		},
		{
			name:      "absent_sqlite_only",
			state:     jsonlAbsent,
			sqlite:    [][]byte{rowHi, rowBye},
			wantLen:   2,
			wantFirst: "hi",
		},
		{
			name:    "absent_no_sqlite_empty",
			state:   jsonlAbsent,
			wantLen: 0,
		},
		{
			name:        "with_messages_migrate_when_sqlite_empty",
			state:       jsonlPresentWithMessages,
			jsonl:       []provider.Message{user},
			wantLen:     1,
			wantFirst:   "hi",
			wantMigrate: true,
		},
		{
			name:      "with_messages_equal_counts_prefer_sqlite",
			state:     jsonlPresentWithMessages,
			jsonl:     []provider.Message{user},
			sqlite:    [][]byte{rowHi},
			wantLen:   1,
			wantFirst: "hi",
		},
		{
			name:        "with_messages_count_mismatch_replace_sqlite",
			state:       jsonlPresentWithMessages,
			jsonl:       []provider.Message{user},
			sqlite:      [][]byte{rowHi, rowBye},
			wantLen:     1,
			wantFirst:   "hi",
			wantReplace: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := reconcileLoad(tc.state, tc.jsonl, tc.sqlite)
			if len(got.Messages) != tc.wantLen {
				t.Fatalf("len=%d want %d msgs=%v", len(got.Messages), tc.wantLen, got.Messages)
			}
			if tc.wantLen > 0 && got.Messages[0].Content != tc.wantFirst {
				t.Fatalf("first=%q want %q", got.Messages[0].Content, tc.wantFirst)
			}
			if got.ClearSQLite != tc.wantClear {
				t.Fatalf("ClearSQLite=%v want %v", got.ClearSQLite, tc.wantClear)
			}
			if got.ReplaceSQLite != tc.wantReplace {
				t.Fatalf("ReplaceSQLite=%v want %v", got.ReplaceSQLite, tc.wantReplace)
			}
			if got.MigrateJSONL != tc.wantMigrate {
				t.Fatalf("MigrateJSONL=%v want %v", got.MigrateJSONL, tc.wantMigrate)
			}
			if got.Messages == nil {
				t.Fatal("Messages must be non-nil slice (explicit empty session contract)")
			}
		})
	}
}

func marshalMsg(m provider.Message) ([]byte, error) {
	return json.Marshal(m)
}
