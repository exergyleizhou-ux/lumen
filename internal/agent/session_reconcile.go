package agent

import (
	"encoding/json"

	"lumen/internal/provider"
)

type jsonlFileState int

const (
	jsonlAbsent jsonlFileState = iota
	jsonlPresentEmpty
	jsonlPresentWithMessages
	jsonlUnreadable
)

// loadReconcileResult is the pure decision from JSONL state + SQLite rows.
type loadReconcileResult struct {
	Messages      []provider.Message
	ClearSQLite   bool
	ReplaceSQLite bool
	MigrateJSONL  bool
}

// reconcileLoad decides how to populate session messages on load and what
// SQLite maintenance is required. It does not perform I/O.
func reconcileLoad(state jsonlFileState, jsonlMsgs []provider.Message, sqliteRows [][]byte) loadReconcileResult {
	switch state {
	case jsonlUnreadable:
		if len(sqliteRows) > 0 {
			return loadReconcileResult{Messages: decodeSQLiteRows(sqliteRows)}
		}
		// Explicit empty session; do not wipe sqlite when there is nothing to reconcile.
		return loadReconcileResult{Messages: []provider.Message{}}
	case jsonlPresentEmpty:
		return loadReconcileResult{Messages: []provider.Message{}, ClearSQLite: true}
	case jsonlAbsent:
		if len(sqliteRows) > 0 {
			return loadReconcileResult{Messages: decodeSQLiteRows(sqliteRows)}
		}
		return loadReconcileResult{Messages: []provider.Message{}}
	case jsonlPresentWithMessages:
		if len(jsonlMsgs) == 0 {
			return loadReconcileResult{Messages: []provider.Message{}}
		}
		if len(sqliteRows) > 0 && len(jsonlMsgs) != len(sqliteRows) {
			return loadReconcileResult{Messages: jsonlMsgs, ReplaceSQLite: true}
		}
		if len(sqliteRows) > 0 {
			return loadReconcileResult{Messages: decodeSQLiteRows(sqliteRows)}
		}
		return loadReconcileResult{Messages: jsonlMsgs, MigrateJSONL: true}
	default:
		return loadReconcileResult{Messages: []provider.Message{}}
	}
}

func decodeSQLiteRows(rows [][]byte) []provider.Message {
	var out []provider.Message
	for _, row := range rows {
		var m provider.Message
		if json.Unmarshal(row, &m) == nil {
			out = append(out, m)
		}
	}
	return out
}
