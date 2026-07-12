package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"lumen/internal/computeruse"
	"lumen/internal/tool"
)

func init() {
	tool.RegisterBuiltin(&ScreenCaptureTool{})
	tool.RegisterBuiltin(&MouseClickTool{})
	tool.RegisterBuiltin(&TypeTextTool{})
	tool.RegisterBuiltin(&KeyPressTool{})
	tool.RegisterBuiltin(&OpenAppTool{})
	tool.RegisterBuiltin(&UIInspectTool{})
	tool.RegisterBuiltin(&ComputerStatusTool{})
}

type ScreenCaptureTool struct{}

func (t *ScreenCaptureTool) Name() string   { return "screen_capture" }
func (t *ScreenCaptureTool) ReadOnly() bool { return true }
func (t *ScreenCaptureTool) Description() string {
	return "Take a screenshot of the entire screen. Returns the file path to the PNG. Use this to see what the user is seeing."
}
func (t *ScreenCaptureTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"Optional file path. If empty, saves to a temp file."}}}`)
}
func (t *ScreenCaptureTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Path string }
	json.Unmarshal(args, &p)
	path, err := computeruse.Capture(p.Path)
	if err != nil {
		return "", err
	}
	w, h, _ := computeruse.ScreenSize()
	return fmt.Sprintf("Screenshot saved to %s (%d×%d)", path, w, h), nil
}

type MouseClickTool struct{}

func (t *MouseClickTool) Name() string   { return "click" }
func (t *MouseClickTool) ReadOnly() bool { return false }
func (t *MouseClickTool) Description() string {
	return "Click the mouse at given coordinates (or current position). Use screen_capture first to see the screen, then click on elements."
}
func (t *MouseClickTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"},"button":{"type":"string","enum":["left","right","double"]}},"required":["x","y"]}`)
}
func (t *MouseClickTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		X, Y   int
		Button string
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	switch p.Button {
	case "right":
		if err := computeruse.RightClick(p.X, p.Y); err != nil {
			return "", err
		}
		return fmt.Sprintf("Right-clicked at (%d, %d)", p.X, p.Y), nil
	case "double":
		if err := computeruse.DoubleClick(p.X, p.Y); err != nil {
			return "", err
		}
		return fmt.Sprintf("Double-clicked at (%d, %d)", p.X, p.Y), nil
	default:
		if err := computeruse.Click(p.X, p.Y); err != nil {
			return "", err
		}
		return fmt.Sprintf("Clicked at (%d, %d)", p.X, p.Y), nil
	}
}

type TypeTextTool struct{}

func (t *TypeTextTool) Name() string   { return "type_text" }
func (t *TypeTextTool) ReadOnly() bool { return false }
func (t *TypeTextTool) Description() string {
	return "Type text into the currently focused field. Use after clicking on a text field."
}
func (t *TypeTextTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`)
}
func (t *TypeTextTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Text string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if err := computeruse.TypeText(p.Text); err != nil {
		return "", err
	}
	return fmt.Sprintf("Typed %d characters", len(p.Text)), nil
}

type KeyPressTool struct{}

func (t *KeyPressTool) Name() string   { return "key_press" }
func (t *KeyPressTool) ReadOnly() bool { return false }
func (t *KeyPressTool) Description() string {
	return "Press a key or key combination. Examples: 'return', 'escape', 'command+q', 'command+v', 'shift+tab'."
}
func (t *KeyPressTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"combo":{"type":"string","description":"Key or combo like return, escape, command+v"}},"required":["combo"]}`)
}
func (t *KeyPressTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Combo string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if err := computeruse.KeyPress(p.Combo); err != nil {
		return "", err
	}
	return fmt.Sprintf("Pressed: %s", p.Combo), nil
}

type OpenAppTool struct{}

func (t *OpenAppTool) Name() string   { return "open_app" }
func (t *OpenAppTool) ReadOnly() bool { return false }
func (t *OpenAppTool) Description() string {
	return "Launch or activate a macOS application by name. E.g. 'Safari', 'Terminal', 'Finder', 'Visual Studio Code'."
}
func (t *OpenAppTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"app":{"type":"string"}},"required":["app"]}`)
}
func (t *OpenAppTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ App string }
	if err := json.Unmarshal(args, &p); err != nil {
		return "", err
	}
	if err := computeruse.OpenApp(p.App); err != nil {
		return "", err
	}
	return fmt.Sprintf("Activated: %s", p.App), nil
}

type UIInspectTool struct{}

func (t *UIInspectTool) Name() string   { return "ui_inspect" }
func (t *UIInspectTool) ReadOnly() bool { return true }
func (t *UIInspectTool) Description() string {
	return "Inspect the current window for UI elements via macOS Accessibility API. Returns roles, titles, positions. Optionally filter by role (button, text field, window, etc). Requires Accessibility permission."
}
func (t *UIInspectTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"role":{"type":"string","description":"Filter by UI role: button, text field, window, menu, etc. Empty for all elements."}}}`)
}
func (t *UIInspectTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct{ Role string }
	json.Unmarshal(args, &p)
	elements, err := computeruse.GetUIElements(p.Role)
	if err != nil {
		return "", err
	}
	if len(elements) == 0 {
		return "No matching UI elements found. Make sure Accessibility permission is granted in System Settings.", nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d UI element(s)%s:\n", len(elements), map[bool]string{true: " (filtered by '" + p.Role + "')"}[p.Role != ""])
	for _, e := range elements {
		fmt.Fprintf(&sb, "  [%s] %q at %s size %s", e.Role, e.Title, e.Position, e.Size)
		if e.Focused {
			sb.WriteString(" [FOCUSED]")
		}
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

type ComputerStatusTool struct{}

func (t *ComputerStatusTool) Name() string   { return "computer_status" }
func (t *ComputerStatusTool) ReadOnly() bool { return true }
func (t *ComputerStatusTool) Description() string {
	return "Get current computer status: screen size, mouse position, active application, running apps."
}
func (t *ComputerStatusTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (t *ComputerStatusTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	status := computeruse.Status()
	apps, _ := computeruse.RunningApps()
	var sb strings.Builder
	sb.WriteString(status + "\n\nRunning apps:\n")
	for _, a := range apps {
		sb.WriteString("  " + a + "\n")
	}
	return sb.String(), nil
}
