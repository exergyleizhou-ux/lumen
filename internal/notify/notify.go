// Package notify provides notification channels for agent lifecycle events.
// It supports terminal bell, desktop notifications (stub), webhook POST,
// Slack-compatible messages, and a multi-channel Notifier that fans out.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Channel interface — anything that can deliver a notification.

// Channel is the interface every notification target implements.
type Channel interface {
	// Send delivers a notification.  Returns an error on failure.
	Send(ctx context.Context, msg *Message) error
	// Name returns a human-readable identifier for this channel.
	Name() string
}

// ---------------------------------------------------------------------------
// Message — the universal notification payload.

// Priority indicates urgency.
type Priority int

const (
	PriorityLow    Priority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

func (p Priority) String() string {
	switch p {
	case PriorityLow:
		return "low"
	case PriorityHigh:
		return "high"
	case PriorityCritical:
		return "critical"
	}
	return "normal"
}

// Message is a structured notification.
type Message struct {
	Title     string
	Body      string
	Priority  Priority
	Timestamp time.Time
	Tags      []string
	Link      string // optional URL
	Extra     map[string]interface{}
}

// NewMessage creates a Message with defaults.
func NewMessage(title, body string) *Message {
	return &Message{
		Title:     title,
		Body:      body,
		Priority:  PriorityNormal,
		Timestamp: time.Now(),
	}
}

// WithPriority sets the priority fluently.
func (m *Message) WithPriority(p Priority) *Message {
	m.Priority = p
	return m
}

// WithTags adds tags.
func (m *Message) WithTags(tags ...string) *Message {
	m.Tags = append(m.Tags, tags...)
	return m
}

// Summary returns a one-line summary of the message.
func (m *Message) Summary() string {
	return fmt.Sprintf("[%s] %s: %s", m.Priority.String(), m.Title, m.Body)
}

// ---------------------------------------------------------------------------
// BellChannel — rings the terminal bell (ASCII 7).

// BellChannel writes "\a" to stderr.
type BellChannel struct {
	count int // number of bell characters
}

// NewBellChannel creates a bell channel.
func NewBellChannel() *BellChannel { return &BellChannel{count: 1} }

// SetRepeat configures how many bell characters to emit.
func (bc *BellChannel) SetRepeat(n int) { bc.count = n }

func (bc *BellChannel) Name() string { return "bell" }

func (bc *BellChannel) Send(_ context.Context, msg *Message) error {
	_ = msg // for interface compliance; bell ignores content
	bell := strings.Repeat("\a", bc.count)
	_, err := os.Stderr.WriteString(bell)
	return err
}

// ---------------------------------------------------------------------------
// DesktopChannel — stub for desktop notifications (osascript / notify-send).

// DesktopChannel tries to invoke the platform desktop notifier.
type DesktopChannel struct {
	appName string
}

// NewDesktopChannel creates a desktop notification stub.  On macOS it will
// attempt osascript; on Linux, notify-send.  The implementation is best-effort.
func NewDesktopChannel(appName string) *DesktopChannel {
	return &DesktopChannel{appName: appName}
}

func (dc *DesktopChannel) Name() string { return "desktop" }

func (dc *DesktopChannel) Send(ctx context.Context, msg *Message) error {
	title := strings.ReplaceAll(msg.Title, "\"", "'")
	body := strings.ReplaceAll(msg.Body, "\"", "'")
	// Try osascript (macOS).
	if _, err := exec.LookPath("osascript"); err == nil {
		script := fmt.Sprintf(
			`display notification "%s" with title "%s"`,
			body, title,
		)
		return exec.CommandContext(ctx, "osascript", "-e", script).Run()
	}
	// Try notify-send (Linux).
	if _, err := exec.LookPath("notify-send"); err == nil {
		return exec.CommandContext(ctx, "notify-send", title, body, "-a", dc.appName).Run()
	}
	// Neither available — not an error.
	return nil
}

// ---------------------------------------------------------------------------
// WebhookChannel — POSTs a JSON payload to a URL.

// WebhookChannel delivers notifications via HTTP POST.
type WebhookChannel struct {
	URL     string
	client  *http.Client
	headers map[string]string
	mu      sync.Mutex
	retries int
}

// NewWebhookChannel creates a webhook channel.
func NewWebhookChannel(url string) *WebhookChannel {
	return &WebhookChannel{
		URL:     url,
		client:  &http.Client{Timeout: 10 * time.Second},
		headers: map[string]string{"Content-Type": "application/json"},
		retries: 2,
	}
}

// SetHeader adds a custom HTTP header for every request.
func (wc *WebhookChannel) SetHeader(k, v string) {
	wc.mu.Lock()
	defer wc.mu.Unlock()
	wc.headers[k] = v
}

// SetRetries configures the number of retry attempts on failure.
func (wc *WebhookChannel) SetRetries(n int) { wc.retries = n }

func (wc *WebhookChannel) Name() string { return "webhook:" + wc.URL }

func (wc *WebhookChannel) Send(ctx context.Context, msg *Message) error {
	payload := map[string]interface{}{
		"title":     msg.Title,
		"body":      msg.Body,
		"priority":  msg.Priority.String(),
		"timestamp": msg.Timestamp.Format(time.RFC3339),
		"tags":      msg.Tags,
		"link":      msg.Link,
	}
	if msg.Extra != nil {
		payload["extra"] = msg.Extra
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("notify: marshal: %w", err)
	}
	var lastErr error
	for attempt := 0; attempt <= wc.retries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, wc.URL, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("notify: request: %w", err)
		}
		wc.mu.Lock()
		for k, v := range wc.headers {
			req.Header.Set(k, v)
		}
		wc.mu.Unlock()
		resp, err := wc.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("notify: webhook returned %d", resp.StatusCode)
	}
	return lastErr
}

// ---------------------------------------------------------------------------
// SlackFormatter — formats a Message as a Slack Block Kit payload.

// SlackFormatter produces Slack-compatible JSON.
type SlackFormatter struct {
	channel  string
	username string
	icon     string
}

// NewSlackFormatter creates a Slack formatter.
func NewSlackFormatter(channel string) *SlackFormatter {
	return &SlackFormatter{channel: channel}
}

// SetUsername overrides the bot name.
func (sf *SlackFormatter) SetUsername(u string) { sf.username = u }

// SetIcon overrides the bot icon (emoji or URL).
func (sf *SlackFormatter) SetIcon(i string) { sf.icon = i }

// Format returns a JSON payload suitable for POSTing to a Slack webhook URL,
// using Block Kit formatting.
func (sf *SlackFormatter) Format(msg *Message) ([]byte, error) {
	color := "#36a64f" // green for normal
	switch msg.Priority {
	case PriorityLow:
		color = "#cccccc"
	case PriorityHigh:
		color = "#ff9900"
	case PriorityCritical:
		color = "#ff0000"
	}
	blocks := []map[string]interface{}{
		{
			"type": "header",
			"text": map[string]interface{}{
				"type": "plain_text",
				"text": msg.Title,
			},
		},
		{
			"type": "section",
			"text": map[string]interface{}{
				"type": "mrkdwn",
				"text": msg.Body,
			},
		},
	}
	if len(msg.Tags) > 0 {
		blocks = append(blocks, map[string]interface{}{
			"type": "context",
			"elements": []map[string]interface{}{
				{
					"type": "mrkdwn",
					"text": "Tags: " + strings.Join(msg.Tags, ", "),
				},
			},
		})
	}
	payload := map[string]interface{}{
		"text":   msg.Title, // fallback text
		"blocks": blocks,
		"attachments": []map[string]interface{}{
			{
				"color":      color,
				"footer":     "Lumen Agent",
				"ts":         msg.Timestamp.Unix(),
				"title":      msg.Title,
				"title_link": msg.Link,
				"text":       msg.Body,
			},
		},
	}
	if sf.channel != "" {
		payload["channel"] = sf.channel
	}
	if sf.username != "" {
		payload["username"] = sf.username
	}
	if sf.icon != "" {
		if strings.HasPrefix(sf.icon, ":") {
			payload["icon_emoji"] = sf.icon
		} else {
			payload["icon_url"] = sf.icon
		}
	}
	return json.Marshal(payload)
}

// ---------------------------------------------------------------------------
// SlackChannel — sends messages to a Slack incoming webhook.

// SlackChannel implements Channel for Slack.
type SlackChannel struct {
	webhookURL string
	formatter  *SlackFormatter
	client     *http.Client
}

// NewSlackChannel creates a Slack channel targeting a webhook URL.
func NewSlackChannel(webhookURL, channel string) *SlackChannel {
	return &SlackChannel{
		webhookURL: webhookURL,
		formatter:  NewSlackFormatter(channel),
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (sc *SlackChannel) Name() string { return "slack:" + sc.formatter.channel }

func (sc *SlackChannel) Send(ctx context.Context, msg *Message) error {
	payload, err := sc.formatter.Format(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sc.webhookURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := sc.client.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("notify: slack returned %d", resp.StatusCode)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Notifier — fans out messages to multiple channels.

// Notifier holds a set of Channels and delivers to all of them.
type Notifier struct {
	channels []Channel
	mu       sync.RWMutex
}

// NewNotifier creates a Notifier, optionally with initial channels.
func NewNotifier(channels ...Channel) *Notifier {
	return &Notifier{channels: channels}
}

// Add registers a new channel.
func (n *Notifier) Add(ch Channel) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.channels = append(n.channels, ch)
}

// Remove removes all channels with the given name.
func (n *Notifier) Remove(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	filtered := n.channels[:0]
	for _, ch := range n.channels {
		if ch.Name() != name {
			filtered = append(filtered, ch)
		}
	}
	n.channels = filtered
}

// Channels returns a snapshot of registered channel names.
func (n *Notifier) Channels() []string {
	n.mu.RLock()
	defer n.mu.RUnlock()
	names := make([]string, len(n.channels))
	for i, ch := range n.channels {
		names[i] = ch.Name()
	}
	return names
}

// Send delivers a message to every registered channel concurrently.  It
// returns the first error encountered, but always attempts all channels.
func (n *Notifier) Send(ctx context.Context, msg *Message) error {
	n.mu.RLock()
	chs := make([]Channel, len(n.channels))
	copy(chs, n.channels)
	n.mu.RUnlock()

	var wg sync.WaitGroup
	errCh := make(chan error, len(chs))
	for _, ch := range chs {
		wg.Add(1)
		go func(c Channel) {
			defer wg.Done()
			if err := c.Send(ctx, msg); err != nil {
				errCh <- fmt.Errorf("%s: %w", c.Name(), err)
			}
		}(ch)
	}
	wg.Wait()
	close(errCh)

	var firstErr error
	for e := range errCh {
		if firstErr == nil {
			firstErr = e
		}
	}
	return firstErr
}

// SendSync delivers sequentially (simpler but slower).
func (n *Notifier) SendSync(ctx context.Context, msg *Message) error {
	n.mu.RLock()
	defer n.mu.RUnlock()
	var firstErr error
	for _, ch := range n.channels {
		if err := ch.Send(ctx, msg); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Close releases resources held by channels that implement io.Closer.
func (n *Notifier) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	var firstErr error
	for _, ch := range n.channels {
		if c, ok := ch.(io.Closer); ok {
			if err := c.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// ---------------------------------------------------------------------------
// FileChannel — appends notifications to a log file.

// FileChannel writes each notification as a JSON line to a file.
type FileChannel struct {
	path string
	f    *os.File
	mu   sync.Mutex
}

// NewFileChannel opens (or creates) a file for appending notifications.
func NewFileChannel(path string) (*FileChannel, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &FileChannel{path: path, f: f}, nil
}

func (fc *FileChannel) Name() string { return "file:" + fc.path }

func (fc *FileChannel) Send(_ context.Context, msg *Message) error {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	enc := json.NewEncoder(fc.f)
	enc.SetEscapeHTML(false)
	return enc.Encode(msg)
}

func (fc *FileChannel) Close() error {
	fc.mu.Lock()
	defer fc.mu.Unlock()
	return fc.f.Close()
}

// ---------------------------------------------------------------------------
// Test (stub) channel that records messages in memory.

// BufferChannel captures messages in a slice for inspection.
type BufferChannel struct {
	mu       sync.Mutex
	messages []*Message
}

// NewBufferChannel creates an in-memory buffer channel.
func NewBufferChannel() *BufferChannel { return &BufferChannel{} }

func (bc *BufferChannel) Name() string { return "buffer" }

func (bc *BufferChannel) Send(_ context.Context, msg *Message) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.messages = append(bc.messages, msg)
	return nil
}

// Messages returns a copy of all captured messages.
func (bc *BufferChannel) Messages() []*Message {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	out := make([]*Message, len(bc.messages))
	copy(out, bc.messages)
	return out
}

// Clear empties the buffer.
func (bc *BufferChannel) Clear() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.messages = nil
}
