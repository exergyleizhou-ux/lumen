package tui

import "testing"

// A tool's dispatch and result are sent as two TuiMsgs sharing a Step. They must
// coalesce into ONE chat row (updated in place), not two duplicate rows.
func TestAddChatEntry_CoalescesToolDispatchAndResult(t *testing.T) {
	m := NewModel()
	m.addChatEntry(TuiMsg{Role: "tool", ToolCalls: []ToolCall{{Name: "read_file", Status: "running", Step: 1}}})
	m.addChatEntry(TuiMsg{Role: "tool", ToolCalls: []ToolCall{{Name: "read_file", Output: "data", Status: "done", Step: 1}}})

	tools := 0
	var last ChatEntry
	for _, e := range m.chat.entries {
		if e.Kind == "tool" {
			tools++
			last = e
		}
	}
	if tools != 1 {
		t.Fatalf("dispatch+result for one tool should coalesce to 1 row, got %d", tools)
	}
	if last.Tool.Status != "done" || last.Tool.Output != "data" {
		t.Errorf("coalesced row should carry the result status+output, got %+v", last.Tool)
	}
}
