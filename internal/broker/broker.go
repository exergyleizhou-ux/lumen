// Package broker provides a message broker abstraction for pub/sub,
// request/reply, and streaming patterns. It normalizes NATS, Redis, and
// in-memory backends behind a single interface for agent communication.
package broker

import ("context";"encoding/json";"fmt";"sort";"strings";"sync";"time")

// Message is a broker-delivered message.
type Message struct {
	ID        string            `json:"id"`
	Subject   string            `json:"subject"`
	Data      []byte            `json:"data"`
	Headers   map[string]string `json:"headers,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	ReplyTo   string            `json:"reply_to,omitempty"`
}

// Subscriber receives messages on a subject.
type Subscriber interface {
	OnMessage(msg *Message)
	Subject() string
	QueueGroup() string
}

// Broker is the message broker interface.
type Broker interface {
	Publish(ctx context.Context, subject string, data []byte) error
	Subscribe(sub Subscriber) error
	Unsubscribe(sub Subscriber) error
	Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (*Message, error)
	Close() error
	Name() string
}

// MemoryBroker is an in-memory broker for testing and single-process use.
type MemoryBroker struct {
	mu          sync.RWMutex
	subscribers map[string][]Subscriber
	counter     int64
	closed      bool
	replyChs    map[string]chan *Message
	replyMu     sync.Mutex
}

// NewMemoryBroker creates an in-memory broker.
func NewMemoryBroker() *MemoryBroker {
	return &MemoryBroker{subscribers: map[string][]Subscriber{}, replyChs: map[string]chan *Message{}}
}

func (mb *MemoryBroker) Name() string { return "memory" }

func (mb *MemoryBroker) Publish(ctx context.Context, subject string, data []byte) error {
	mb.mu.RLock(); defer mb.mu.RUnlock()
	if mb.closed { return fmt.Errorf("broker closed") }

	mb.counter++
	msg := &Message{ID: fmt.Sprintf("msg-%d", mb.counter), Subject: subject, Data: data, Timestamp: time.Now()}

	for _, sub := range mb.subscribers[subject] {
		go sub.OnMessage(msg)
	}
	return nil
}

func (mb *MemoryBroker) Subscribe(sub Subscriber) error {
	mb.mu.Lock(); defer mb.mu.Unlock()
	if mb.closed { return fmt.Errorf("broker closed") }
	mb.subscribers[sub.Subject()] = append(mb.subscribers[sub.Subject()], sub)
	return nil
}

func (mb *MemoryBroker) Unsubscribe(sub Subscriber) error {
	mb.mu.Lock(); defer mb.mu.Unlock()
	subs := mb.subscribers[sub.Subject()]
	for i, s := range subs {
		if s == sub {
			mb.subscribers[sub.Subject()] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	return nil
}

func (mb *MemoryBroker) Request(ctx context.Context, subject string, data []byte, timeout time.Duration) (*Message, error) {
	replyCh := make(chan *Message, 1)
	mb.counter++
	replyID := fmt.Sprintf("_reply.%d", mb.counter)

	mb.replyMu.Lock()
	mb.replyChs[replyID] = replyCh
	mb.replyMu.Unlock()

	defer func() {
		mb.replyMu.Lock()
		delete(mb.replyChs, replyID)
		mb.replyMu.Unlock()
	}()

	if err := mb.Publish(ctx, subject, data); err != nil { return nil, err }

	select {
	case reply := <-replyCh:
		return reply, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("request timed out after %v", timeout)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (mb *MemoryBroker) Close() error {
	mb.mu.Lock(); defer mb.mu.Unlock()
	mb.closed = true
	mb.subscribers = nil
	return nil
}

// Respond sends a reply to a request message.
func (mb *MemoryBroker) Respond(replyID string, data []byte) {
	mb.replyMu.Lock()
	ch, ok := mb.replyChs[replyID]
	mb.replyMu.Unlock()
	if ok {
		ch <- &Message{ID: replyID, Data: data, Timestamp: time.Now()}
	}
}

// ── Message Router ────────────────────────────────────────

// Router routes messages by subject pattern.
type Router struct {
	mu     sync.RWMutex
	routes map[string]func(*Message) *Message
	broker Broker
}

// NewRouter creates a message router.
func NewRouter(b Broker) *Router {
	return &Router{routes: map[string]func(*Message) *Message{}, broker: b}
}

// Handle registers a handler for a subject.
func (r *Router) Handle(subject string, handler func(*Message) *Message) {
	r.mu.Lock(); defer r.mu.Unlock()
	r.routes[subject] = handler
}

// Start begins listening on all registered subjects.
func (r *Router) Start() error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for subject := range r.routes {
		s := &routerSubscriber{subject: subject, router: r}
		if err := r.broker.Subscribe(s); err != nil { return err }
	}
	return nil
}

type routerSubscriber struct {
	subject string
	router  *Router
}

func (rs *routerSubscriber) Subject() string { return rs.subject }
func (rs *routerSubscriber) QueueGroup() string { return "" }
func (rs *routerSubscriber) OnMessage(msg *Message) {
	rs.router.mu.RLock()
	handler, ok := rs.router.routes[msg.Subject]
	rs.router.mu.RUnlock()
	if ok {
		reply := handler(msg)
		if reply != nil {
			rs.router.broker.Publish(context.Background(), msg.ReplyTo, reply.Data)
		}
	}
}

// ── Stats Collector ──────────────────────────────────────

// Stats tracks broker metrics.
type Stats struct {
	Published  int64            `json:"published"`
	Received   int64            `json:"received"`
	BySubject  map[string]int64 `json:"by_subject"`
	mu         sync.Mutex
}

// NewStats creates a stats collector.
func NewStats() *Stats { return &Stats{BySubject: map[string]int64{}} }

// RecordPublish records a published message.
func (s *Stats) RecordPublish(subject string) {
	s.mu.Lock(); defer s.mu.Unlock()
	s.Published++
	s.BySubject[subject]++
}

// RecordReceive records a received message.
func (s *Stats) RecordReceive(subject string) {
	s.mu.Lock(); defer s.mu.Unlock()
	s.Received++
}

// FormatStats formats broker statistics.
func (s *Stats) FormatStats() string {
	s.mu.Lock(); defer s.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Broker Stats: %d published, %d received\n%s\n\n", s.Published, s.Received, strings.Repeat("─", 40))
	var keys []string
	for k := range s.BySubject { keys = append(keys, k) }
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "  %-30s %d\n", k, s.BySubject[k])
	}
	return sb.String()
}

// ── JSON Helper ──────────────────────────────────────────

// PublishJSON marshals and publishes a JSON message.
func PublishJSON(ctx context.Context, b Broker, subject string, v any) error {
	data, err := json.Marshal(v)
	if err != nil { return err }
	return b.Publish(ctx, subject, data)
}
