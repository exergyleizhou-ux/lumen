// Package notify sends desktop notifications for long-running agent
// tasks. When a background bash or sub-agent finishes, it fires a system
// notification so the user doesn't need to watch the terminal.
// Adapted from Reasonix's notify/ package.
package notify

import (
	"fmt"
	"os/exec"
	"runtime"
)

// Notify fires a desktop notification with the given title and body.
// On macOS it uses osascript; on Linux it uses notify-send.
// Returns nil if notifications aren't supported.
func Notify(title, body string) error {
	switch runtime.GOOS {
	case "darwin":
		return osascript(title, body)
	case "linux":
		return notifySend(title, body)
	default:
		return fmt.Errorf("notifications not supported on %s", runtime.GOOS)
	}
}

func osascript(title, body string) error {
	script := fmt.Sprintf(`display notification %q with title %q`, body, title)
	return exec.Command("osascript", "-e", script).Run()
}

func notifySend(title, body string) error {
	return exec.Command("notify-send", title, body).Run()
}

// Available reports whether desktop notifications are supported.
func Available() bool {
	switch runtime.GOOS {
	case "darwin":
		_, err := exec.LookPath("osascript")
		return err == nil
	case "linux":
		_, err := exec.LookPath("notify-send")
		return err == nil
	}
	return false
}

// TaskDone fires a notification when a background task completes.
func TaskDone(taskType, label, result string) {
	title := fmt.Sprintf("Lumen: %s complete", taskType)
	body := label
	if result != "" {
		body = fmt.Sprintf("%s — %s", label, truncate(result, 100))
	}
	Notify(title, body)
}

// ApprovalNeeded fires a notification when the agent is waiting for
// tool-call approval.
func ApprovalNeeded(toolName string) {
	Notify("Lumen: approval needed", fmt.Sprintf("Approve %s?", toolName))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
