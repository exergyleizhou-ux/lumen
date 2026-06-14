package agent

import (
	"context"
	"fmt"
	"strings"

	"lumen/internal/provider"
)

// CompactWithModel sends the middle portion of a session to a compact model
// for summarization, then replaces those messages with the summary. This
// preserves semantic context that a sliding-window drop would lose.
//
// The compact model should be cheap and fast (e.g. deepseek-flash).
// If compactProv is nil, it falls back to the sliding-window Compact().
func (a *Agent) CompactWithModel(ctx context.Context, compactProv provider.Provider, keepFirst, keepLast int) error {
	if compactProv == nil || a.session.Len() <= keepFirst+keepLast {
		return nil
	}

	snapshot := a.session.Snapshot()
	if len(snapshot) <= keepFirst+keepLast {
		return nil
	}

	// Extract the middle messages to summarize
	middle := snapshot[keepFirst : len(snapshot)-keepLast]

	// Build a compact prompt
	prompt := buildCompactPrompt(middle)

	// Send to compact model (non-streaming — we want the summary fast)
	ch, err := compactProv.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: compactSystemPrompt},
			{Role: provider.RoleUser, Content: prompt},
		},
		Temperature: 0,
		MaxTokens:   min(keepFirst*200, 4096),
	})
	if err != nil {
		return fmt.Errorf("compact model: %w", err)
	}

	var summary strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			summary.WriteString(chunk.Text)
		case provider.ChunkError:
			return fmt.Errorf("compact stream: %w", chunk.Err)
		}
	}

	summaryText := strings.TrimSpace(summary.String())
	if summaryText == "" {
		return fmt.Errorf("compact model returned empty summary")
	}

	// Replace middle with summary
	a.session.Compact(keepFirst, keepLast, summaryText)
	return nil
}

const compactSystemPrompt = `You are a context-compaction helper for an AI coding agent.
Given a conversation transcript, produce a terse, structured summary of the key facts:

1. **What was requested** — the user's goals and constraints
2. **What was done** — tools that were called, files that were changed, commands run
3. **Current state** — what is finished, what remains, blockers, decisions made
4. **Files touched** — list every file path that was read, written, or edited

Be extremely concise. Use bullet points. Include exact file paths and command strings.
Do not re-execute anything. This summary replaces the original messages, so every
important detail must be preserved.`

func buildCompactPrompt(messages []provider.Message) string {
	var sb strings.Builder
	sb.WriteString("Summarize this conversation segment:\n\n")

	for _, m := range messages {
		switch m.Role {
		case provider.RoleUser:
			if m.Content != "" {
				sb.WriteString("User: ")
				sb.WriteString(truncateForCompact(m.Content, 500))
				sb.WriteByte('\n')
			}
		case provider.RoleAssistant:
			if m.Content != "" {
				sb.WriteString("Assistant: ")
				sb.WriteString(truncateForCompact(m.Content, 300))
				sb.WriteByte('\n')
			}
			for _, tc := range m.ToolCalls {
				sb.WriteString("  → called ")
				sb.WriteString(tc.Name)
				sb.WriteByte('\n')
			}
		case provider.RoleTool:
			content := truncateForCompact(m.Content, 200)
			if content != "" {
				sb.WriteString("Tool result (")
				sb.WriteString(m.Name)
				sb.WriteString("): ")
				sb.WriteString(content)
				sb.WriteByte('\n')
			}
		case provider.RoleSystem:
			// skip system messages in the compact prompt
		}
	}
	return sb.String()
}

func truncateForCompact(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
