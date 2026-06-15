// Package computeruse provides screen capture, mouse/keyboard control,
// and accessibility integration for Lumen agents on macOS.
// Uses native tools: screencapture, osascript (AppleScript), and
// optionally cliclick for precision mouse control.
package computeruse

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// ── Screen ─────────────────────────────────────────────────

// ScreenSize returns the current screen dimensions in pixels.
func ScreenSize() (width, height int, err error) {
	out, err := exec.Command("osascript", "-e",
		`tell application "Finder" to get bounds of window of desktop`).Output()
	if err != nil {
		// Fallback: use system_profiler
		out2, err2 := exec.Command("system_profiler", "SPDisplaysDataType").Output()
		if err2 != nil {
			return 0, 0, fmt.Errorf("screen size: %w / %w", err, err2)
		}
		// Parse "Resolution: 2560 x 1664"
		lines := string(out2)
		for _, line := range strings.Split(lines, "\n") {
			if strings.Contains(line, "Resolution:") {
				parts := strings.Fields(line)
				if len(parts) >= 4 {
					w, _ := strconv.Atoi(parts[1])
					h, _ := strconv.Atoi(parts[3])
					if w > 0 && h > 0 {
						return w, h, nil
					}
				}
			}
		}
		return 0, 0, fmt.Errorf("could not parse resolution")
	}
	// osascript returns e.g. "0, 0, 2560, 1664"
	parts := strings.Split(strings.TrimSpace(string(out)), ", ")
	if len(parts) >= 4 {
		// format: left, top, right, bottom
		_, _ = strconv.Atoi(parts[0]) // left
		_, _ = strconv.Atoi(parts[1]) // top
		right, err1 := strconv.Atoi(parts[2])
		bottom, err2 := strconv.Atoi(parts[3])
		if err1 == nil && err2 == nil {
			return right, bottom, nil
		}
	}
	return 0, 0, fmt.Errorf("could not parse: %s", string(out))
}

// Capture takes a screenshot and saves to the given path (PNG).
// If path is "", saves to a temp file and returns the path.
func Capture(path string) (string, error) {
	if path == "" {
		f, err := os.CreateTemp("", "lumen-screenshot-*.png")
		if err != nil {
			return "", err
		}
		path = f.Name()
		f.Close()
	}

	cmd := exec.Command("screencapture", "-x", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("screencapture: %w: %s", err, string(out))
	}

	return path, nil
}

// CaptureBase64 captures a screenshot and returns it as a base64-encoded PNG
// data URI suitable for embedding.
func CaptureBase64() (string, error) {
	path, err := Capture("")
	if err != nil {
		return "", err
	}
	defer os.Remove(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	b64 := base64.StdEncoding.EncodeToString(data)
	return "data:image/png;base64," + b64, nil
}

// ── Mouse ──────────────────────────────────────────────────

// MoveMouse moves the mouse cursor to absolute coordinates.
func MoveMouse(x, y int) error {
	script := fmt.Sprintf(
		`tell application "System Events" to set position of first mouse to {%d, %d}`,
		x, y)
	return runAppleScript(script)
}

// Click performs a mouse click at current position (or at x,y if specified).
func Click(x, y int) error {
	if x >= 0 && y >= 0 {
		if err := MoveMouse(x, y); err != nil {
			return err
		}
	}
	// Click via AppleScript: "click at" + use mouse down/up
	script := fmt.Sprintf(`
		tell application "System Events"
			set mousePos to position of first mouse
			set x to item 1 of mousePos
			set y to item 2 of mousePos
			click at {x, y}
		end tell`)
	return runAppleScript(script)
}

// DoubleClick performs a double-click.
func DoubleClick(x, y int) error {
	if x >= 0 && y >= 0 {
		if err := MoveMouse(x, y); err != nil {
			return err
		}
	}
	script := fmt.Sprintf(`
		tell application "System Events"
			set mousePos to position of first mouse
			set x to item 1 of mousePos
			set y to item 2 of mousePos
			double click at {x, y}
		end tell`)
	return runAppleScript(script)
}

// RightClick performs a right-click (control-click).
func RightClick(x, y int) error {
	if x >= 0 && y >= 0 {
		if err := MoveMouse(x, y); err != nil {
			return err
		}
	}
	script := fmt.Sprintf(`
		tell application "System Events"
			set mousePos to position of first mouse
			set x to item 1 of mousePos
			set y to item 2 of mousePos
			click at {x, y} with control down
		end tell`)
	return runAppleScript(script)
}

// Drag performs a mouse drag from (x1,y1) to (x2,y2).
func Drag(x1, y1, x2, y2 int) error {
	script := fmt.Sprintf(`
		set startPos to {%d, %d}
		set endPos to {%d, %d}
		tell application "System Events"
			set position of first mouse to startPos
			delay 0.1
			set steps to 10
			set dx to (item 1 of endPos) - (item 1 of startPos)
			set dy to (item 2 of endPos) - (item 2 of startPos)
			repeat with i from 0 to steps
				set px to (item 1 of startPos) + (dx * i / steps)
				set py to (item 2 of startPos) + (dy * i / steps)
				set position of first mouse to {px as integer, py as integer}
				delay 0.01
			end repeat
		end tell`, x1, y1, x2, y2)
	return runAppleScript(script)
}

// Scroll scrolls the mouse wheel at current position.
// amount: positive = up, negative = down. Lines to scroll.
func Scroll(amount int) error {
	// AppleScript can't directly scroll; use key code for Page Up/Down
	// Or use mouse scroll events via CGEvent (need a helper)
	// For now: use Arrow Up/Down with Option for page scroll
	key := 126 // Up arrow
	if amount < 0 {
		key = 125 // Down arrow
		amount = -amount
	}
	for i := 0; i < amount && i < 10; i++ {
		script := fmt.Sprintf(`tell application "System Events" to key code %d`, key)
		if err := runAppleScript(script); err != nil {
			return err
		}
	}
	return nil
}

// MousePosition returns the current mouse (x, y).
func MousePosition() (x, y int, err error) {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get position of first mouse`).Output()
	if err != nil {
		return 0, 0, err
	}
	// output: "123, 456"
	parts := strings.Split(strings.TrimSpace(string(out)), ", ")
	if len(parts) == 2 {
		x, _ = strconv.Atoi(parts[0])
		y, _ = strconv.Atoi(parts[1])
		return x, y, nil
	}
	return 0, 0, fmt.Errorf("parse: %s", string(out))
}

// ── Keyboard ───────────────────────────────────────────────

// TypeText types a string character by character via AppleScript.
func TypeText(text string) error {
	// Escape quotes and backslashes for AppleScript
	escaped := strings.ReplaceAll(text, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escaped)
	return runAppleScript(script)
}

// KeyPress simulates pressing a single key or key combo.
// Examples: "return", "space", "tab", "escape", "command+v", "command+shift+3"
func KeyPress(combo string) error {
	combo = strings.ToLower(strings.TrimSpace(combo))

	// Handle combinations like "command+v", "ctrl+c", "shift+tab"
	parts := strings.Split(combo, "+")
	key := parts[len(parts)-1]

	modifiers := map[string]string{
		"command": "command down",
		"cmd":     "command down",
		"shift":   "shift down",
		"option":  "option down",
		"alt":     "option down",
		"control": "control down",
		"ctrl":    "control down",
		"fn":      "function down",
	}

	var modList []string
	for i := 0; i < len(parts)-1; i++ {
		if m, ok := modifiers[parts[i]]; ok {
			modList = append(modList, m)
		}
	}

	if len(modList) > 0 {
		script := fmt.Sprintf(`tell application "System Events" to keystroke "%s" using {%s}`,
			key, strings.Join(modList, ", "))
		return runAppleScript(script)
	}

	// Single key
	script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, key)
	return runAppleScript(script)
}

// ── Application Control ────────────────────────────────────

// OpenApp launches or activates an application by name.
func OpenApp(name string) error {
	script := fmt.Sprintf(`tell application "%s" to activate`, name)
	return runAppleScript(script)
}

// ActiveApp returns the name of the frontmost application.
func ActiveApp() (string, error) {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get name of first process whose frontmost is true`).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RunningApps returns a list of running application names.
func RunningApps() ([]string, error) {
	out, err := exec.Command("osascript", "-e",
		`tell application "System Events" to get name of every process whose background only is false`).Output()
	if err != nil {
		return nil, err
	}
	// Output: "App1, App2, App3"
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ", ")
	return parts, nil
}

// ── Accessibility ──────────────────────────────────────────

// UIElement represents a UI element found via Accessibility API.
type UIElement struct {
	Role        string `json:"role"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Position    string `json:"position"` // "x,y"
	Size        string `json:"size"`     // "w,h"
	Focused     bool   `json:"focused"`
}

// GetUIElements returns UI elements matching the given role filter.
// Role can be empty for all elements, or e.g. "button", "text field", "window".
func GetUIElements(role string) ([]UIElement, error) {
	var script string
	if role == "" {
		script = `
		tell application "System Events"
			set output to ""
			tell process 1 where frontmost is true
				repeat with elem in entire contents of window 1
					try
						set output to output & (role of elem) & "|" & (title of elem) & "|" & (description of elem) & "|" & ((position of elem) as text) & "|" & ((size of elem) as text) & "|" & (focused of elem) & linefeed
					end try
				end repeat
			end tell
			return output
		end tell`
	} else {
		script = fmt.Sprintf(`
		tell application "System Events"
			set output to ""
			tell process 1 where frontmost is true
				set elems to every %s of window 1
				repeat with elem in elems
					try
						set output to output & (role of elem) & "|" & (title of elem) & "|" & (description of elem) & "|" & ((position of elem) as text) & "|" & ((size of elem) as text) & "|" & (focused of elem) & linefeed
					end try
				end repeat
			end tell
			return output
		end tell`, role)
	}

	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		return nil, fmt.Errorf("accessibility: %w — grant permission in System Settings > Privacy > Accessibility", err)
	}

	var elements []UIElement
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) < 6 {
			continue
		}
		focused := strings.TrimSpace(parts[5]) == "true"
		elements = append(elements, UIElement{
			Role:        strings.TrimSpace(parts[0]),
			Title:       strings.TrimSpace(parts[1]),
			Description: strings.TrimSpace(parts[2]),
			Position:    strings.TrimSpace(parts[3]),
			Size:        strings.TrimSpace(parts[4]),
			Focused:     focused,
		})
	}
	return elements, nil
}

// ── Helpers ─────────────────────────────────────────────────

func runAppleScript(script string) error {
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("applescript: %w — %s", err, string(out))
	}
	return nil
}

// Status returns a human-readable computer-use capability summary.
func Status() string {
	w, h, err := ScreenSize()
	if err != nil {
		return fmt.Sprintf("Screen: unavailable (%v)", err)
	}
	app, _ := ActiveApp()
	mx, my, _ := MousePosition()
	return fmt.Sprintf(
		"Screen: %d×%d  |  Mouse: (%d, %d)  |  Active app: %s",
		w, h, mx, my, app)
}
