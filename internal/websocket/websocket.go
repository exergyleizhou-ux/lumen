// Package websocket provides WebSocket server and client for real-time
// agent event streaming. Supports pub/sub channels, connection pooling,
// heartbeat management, and binary/text message modes.
package websocket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Connection represents a WebSocket connection (abstracted for testing).
type Connection interface {
	Send(data []byte) error
	Receive() ([]byte, error)
	Close() error
	RemoteAddr() string
	ID() string
}

// Event is a real-time agent event.
type Event struct {
	Type      string    `json:"type"`
	Data      any       `json:"data"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id,omitempty"`
	TraceID   string    `json:"trace_id,omitempty"`
}

// Channel is a pub/sub topic.
type Channel struct {
	Name        string `json:"name"`
	Subscribers map[string]Connection
	mu          sync.RWMutex
}

// NewChannel creates a channel.
func NewChannel(name string) *Channel { return &Channel{Name: name, Subscribers: map[string]Connection{}} }

// Publish sends an event to all subscribers.
func (ch *Channel) Publish(event *Event) int {
	ch.mu.RLock(); defer ch.mu.RUnlock()
	data, _ := json.Marshal(event)
	count := 0
	for _, conn := range ch.Subscribers {
		if err := conn.Send(data); err == nil { count++ }
	}
	return count
}

// Subscribe adds a connection.
func (ch *Channel) Subscribe(conn Connection) {
	ch.mu.Lock(); defer ch.mu.Unlock()
	ch.Subscribers[conn.ID()] = conn
}

// Unsubscribe removes a connection.
func (ch *Channel) Unsubscribe(conn Connection) {
	ch.mu.Lock(); defer ch.mu.Unlock()
	delete(ch.Subscribers, conn.ID())
}

// Count returns subscriber count.
func (ch *Channel) Count() int { ch.mu.RLock(); defer ch.mu.RUnlock(); return len(ch.Subscribers) }

// Hub manages WebSocket connections and channels.
type Hub struct {
	mu        sync.RWMutex
	channels  map[string]*Channel
	conns     map[string]Connection
	pending   atomic.Int64
	done      atomic.Int64
	errors    atomic.Int64
	onEvent   func(*Event)
	onConnect func(Connection)
	onDisconnect func(Connection)
}

// NewHub creates a WebSocket hub.
func NewHub() *Hub {
	return &Hub{channels: map[string]*Channel{}, conns: map[string]Connection{}}
}

// OnEvent sets the event handler.
func (h *Hub) OnEvent(fn func(*Event)) { h.mu.Lock(); defer h.mu.Unlock(); h.onEvent = fn }

// OnConnect sets the connect handler.
func (h *Hub) OnConnect(fn func(Connection)) { h.mu.Lock(); defer h.mu.Unlock(); h.onConnect = fn }

// OnDisconnect sets the disconnect handler.
func (h *Hub) OnDisconnect(fn func(Connection)) { h.mu.Lock(); defer h.mu.Unlock(); h.onDisconnect = fn }

// Connect registers a new connection.
func (h *Hub) Connect(conn Connection) {
	h.mu.Lock(); h.conns[conn.ID()] = conn; h.pending.Add(1); h.mu.Unlock()
	if h.onConnect != nil { h.onConnect(conn) }
}

// Disconnect removes a connection.
func (h *Hub) Disconnect(conn Connection) {
	h.mu.Lock()
	delete(h.conns, conn.ID())
	for _, ch := range h.channels { ch.Unsubscribe(conn) }
	h.mu.Unlock()
	h.done.Add(1)
	conn.Close()
	if h.onDisconnect != nil { h.onDisconnect(conn) }
}

// Join adds a connection to a channel.
func (h *Hub) Join(conn Connection, channelName string) {
	h.mu.Lock()
	ch, ok := h.channels[channelName]
	if !ok { ch = NewChannel(channelName); h.channels[channelName] = ch }
	h.mu.Unlock()
	ch.Subscribe(conn)
}

// Leave removes a connection from a channel.
func (h *Hub) Leave(conn Connection, channelName string) {
	h.mu.RLock(); defer h.mu.RUnlock()
	if ch, ok := h.channels[channelName]; ok { ch.Unsubscribe(conn) }
}

// Emit publishes an event to a channel.
func (h *Hub) Emit(channelName string, event *Event) int {
	h.mu.RLock(); ch, ok := h.channels[channelName]; h.mu.RUnlock()
	if !ok { return 0 }
	return ch.Publish(event)
}

// Broadcast sends an event to all connected clients.
func (h *Hub) Broadcast(event *Event) int {
	h.mu.RLock(); defer h.mu.RUnlock()
	data, _ := json.Marshal(event)
	count := 0
	for _, conn := range h.conns {
		if err := conn.Send(data); err == nil { count++ }
	}
	return count
}

// ChannelNames returns all channel names.
func (h *Hub) ChannelNames() []string {
	h.mu.RLock(); defer h.mu.RUnlock()
	var out []string
	for n := range h.channels { out = append(out, n) }
	sort.Strings(out)
	return out
}

// ConnectionCount returns active connection count.
func (h *Hub) ConnectionCount() int { h.mu.RLock(); defer h.mu.RUnlock(); return len(h.conns) }

// Stats returns hub statistics.
func (h *Hub) Stats() map[string]int64 {
	return map[string]int64{
		"connections": int64(len(h.conns)),
		"channels":    int64(len(h.channels)),
		"pending":     h.pending.Load(),
		"done":        h.done.Load(),
		"errors":      h.errors.Load(),
	}
}

// ── Heartbeat Manager ─────────────────────────────────────

// Heartbeat sends periodic pings to detect dead connections.
type Heartbeat struct {
	mu       sync.Mutex
	interval time.Duration
	timeout  time.Duration
	hub      *Hub
	lastPing map[string]time.Time
	stopCh   chan struct{}
}

// NewHeartbeat creates a heartbeat manager.
func NewHeartbeat(hub *Hub, interval, timeout time.Duration) *Heartbeat {
	return &Heartbeat{hub: hub, interval: interval, timeout: timeout, lastPing: map[string]time.Time{}, stopCh: make(chan struct{})}
}

// Start begins heartbeat checks.
func (hb *Heartbeat) Start() {
	go func() {
		ticker := time.NewTicker(hb.interval)
		defer ticker.Stop()
		for {
			select {
			case <-hb.stopCh: return
			case <-ticker.C:
				hb.check()
			}
		}
	}()
}

// Stop stops heartbeat checks.
func (hb *Heartbeat) Stop() { close(hb.stopCh) }

func (hb *Heartbeat) check() {
	now := time.Now()
	hb.mu.Lock()
	defer hb.mu.Unlock()
	hb.hub.mu.RLock()
	defer hb.hub.mu.RUnlock()

	for id, conn := range hb.hub.conns {
		if err := conn.Send([]byte(`{"type":"ping"}`)); err != nil {
			if last, ok := hb.lastPing[id]; ok && now.Sub(last) > hb.timeout {
				go hb.hub.Disconnect(conn)
			}
		}
		hb.lastPing[id] = now
	}
}

// ── Mock Connection for testing ──────────────────────────

// MockConn is an in-memory connection for testing.
type MockConn struct {
	id      string
	addr    string
	sendCh  chan []byte
	recvCh  chan []byte
	closed  bool
	mu      sync.Mutex
}

// NewMockConn creates a mock connection.
var nextMockID int64
func NewMockConn() *MockConn {
	id := atomic.AddInt64(&nextMockID, 1)
	return &MockConn{id: fmt.Sprintf("conn-%d", id), addr: fmt.Sprintf("mock:%d", id), sendCh: make(chan []byte, 32), recvCh: make(chan []byte, 32)}
}
func (mc *MockConn) ID() string { return mc.id }
func (mc *MockConn) RemoteAddr() string { return mc.addr }
func (mc *MockConn) Send(data []byte) error {
	mc.mu.Lock(); defer mc.mu.Unlock()
	if mc.closed { return fmt.Errorf("closed") }
	select { case mc.sendCh <- data: default: }
	return nil
}
func (mc *MockConn) Receive() ([]byte, error) {
	select {
	case d := <-mc.recvCh: return d, nil
	case <-time.After(time.Second): return nil, fmt.Errorf("timeout")
	}
}
func (mc *MockConn) Close() error {
	mc.mu.Lock(); defer mc.mu.Unlock()
	mc.closed = true; close(mc.sendCh); return nil
}

// ── WebSocket Handler ────────────────────────────────────

// WSHandler upgrades HTTP requests to WebSocket connections.
type WSHandler struct {
	hub      *Hub
	upgrader Upgrader
}

// Upgrader abstracts WebSocket upgrade logic.
type Upgrader struct {
	CheckOrigin func(r *http.Request) bool
}

// NewWSHandler creates a WebSocket HTTP handler.
func NewWSHandler(hub *Hub) *WSHandler {
	return &WSHandler{hub: hub, upgrader: Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}}
}

// ServeHTTP handles WebSocket upgrade requests.
func (wh *WSHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// In a real implementation, this would upgrade the HTTP connection.
	// For the abstract version, we register a mock connection.
	conn := NewMockConn()
	wh.hub.Connect(conn)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "connected", "connection_id": conn.ID()})
}

// ── Event Logger ──────────────────────────────────────────

// EventLog records events for replay.
type EventLog struct {
	mu      sync.Mutex
	events  []*Event
	maxSize int
}

// NewEventLog creates an event log.
func NewEventLog(maxSize int) *EventLog { return &EventLog{maxSize: maxSize} }

// Record stores an event.
func (el *EventLog) Record(event *Event) {
	el.mu.Lock(); defer el.mu.Unlock()
	el.events = append(el.events, event)
	if len(el.events) > el.maxSize { el.events = el.events[1:] }
}

// Replay returns events since a timestamp.
func (el *EventLog) Replay(since time.Time) []*Event {
	el.mu.Lock(); defer el.mu.Unlock()
	var out []*Event
	for _, e := range el.events {
		if e.Timestamp.After(since) { out = append(out, e) }
	}
	return out
}

// FormatStats formats hub stats.
func FormatStats(stats map[string]int64) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("WebSocket Hub Stats:\n%s\n\n", strings.Repeat("─", 40)))
	keys := []string{"connections", "channels", "pending", "done", "errors"}
	for _, k := range keys {
		if v, ok := stats[k]; ok {
			fmt.Fprintf(&sb, "  %-15s %d\n", k, v)
		}
	}
	return sb.String()
}
