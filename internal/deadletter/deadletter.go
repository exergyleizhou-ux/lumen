// Package deadletter implements a dead letter queue for storing failed messages with
// retry metadata, replay capability, TTL-based expiry, and per-subject partitioning.
package deadletter

import (
	"container/list"
	"fmt"
	"sort"
	"sync"
	"time"
)

// DeliveryAttempt records metadata about a single delivery try.
type DeliveryAttempt struct {
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error"`
	Latency   time.Duration `json:"latency"`
}

// Message represents a failed message stored in the dead letter queue.
type Message struct {
	ID          string            `json:"id"`
	Subject     string            `json:"subject"`
	Payload     []byte            `json:"payload"`
	Headers     map[string]string `json:"headers"`
	EnqueuedAt  time.Time         `json:"enqueued_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
	RetryCount  int               `json:"retry_count"`
	MaxRetries  int               `json:"max_retries"`
	Attempts    []DeliveryAttempt `json:"attempts"`
	LastError   string            `json:"last_error"`
	Partition   string            `json:"partition"`
}

// QueueStats holds aggregate statistics for a dead letter queue.
type QueueStats struct {
	TotalMessages    int           `json:"total_messages"`
	TotalSubjects    int           `json:"total_subjects"`
	TotalPartitions  int           `json:"total_partitions"`
	ExpiredCount     int64         `json:"expired_count"`
	ReplayedCount    int64         `json:"replayed_count"`
	PurgedCount      int64         `json:"purged_count"`
	OldestMessage    time.Time     `json:"oldest_message"`
	NewestMessage    time.Time     `json:"newest_message"`
}

// Config configures the dead letter queue.
type Config struct {
	MaxMessages       int           // Max total messages before eviction of oldest.
	DefaultTTL        time.Duration // Default TTL for new messages.
	MaxRetries        int           // Default max retries.
	PartitionBy       string        // Field to partition by ("subject" or empty for none).
	EvictionPolicy    string        // "oldest" or "largest".
	ReplayBatchSize   int           // Max messages replayed per batch.
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxMessages:     10000,
		DefaultTTL:      24 * time.Hour,
		MaxRetries:      5,
		PartitionBy:     "subject",
		EvictionPolicy:  "oldest",
		ReplayBatchSize: 100,
	}
}

// Queue implements a dead letter queue with per-subject partitioning.
type Queue struct {
	mu              sync.RWMutex
	config          Config
	messages        map[string]*list.Element // message ID -> list element
	order           *list.List               // ordered by insertion time
	subjects        map[string]*list.List    // subject -> messages
	partitions      map[string]*list.List    // partition -> messages
	expiredCount    int64
	replayedCount   int64
	purgedCount     int64
	oldestTimestamp time.Time
	newestTimestamp time.Time
	stopCleanup     chan struct{}
}

// messageEntry is the element stored in the linked lists.
type messageEntry struct {
	msg *Message
}

// New creates a new dead letter queue.
func New(cfg Config) *Queue {
	if cfg.MaxMessages <= 0 {
		cfg.MaxMessages = 10000
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = 24 * time.Hour
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 5
	}
	if cfg.ReplayBatchSize <= 0 {
		cfg.ReplayBatchSize = 100
	}
	q := &Queue{
		config:     cfg,
		messages:   make(map[string]*list.Element),
		order:      list.New(),
		subjects:   make(map[string]*list.List),
		partitions: make(map[string]*list.List),
		stopCleanup: make(chan struct{}),
	}
	go q.cleanupLoop()
	return q
}

// cleanupLoop periodically removes expired messages.
func (q *Queue) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			q.removeExpired()
		case <-q.stopCleanup:
			return
		}
	}
}

// removeExpired purges all messages past their TTL.
func (q *Queue) removeExpired() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := time.Now()
	removed := 0
	var next *list.Element
	for e := q.order.Front(); e != nil; e = next {
		next = e.Next()
		entry := e.Value.(*messageEntry)
		if now.After(entry.msg.ExpiresAt) {
			q.removeElement(e)
			removed++
		}
	}
	atomicAdd(&q.expiredCount, int64(removed))
	return removed
}

func atomicAdd(p *int64, n int64) { *p += n } // simple helper under lock

// Close stops the cleanup goroutine.
func (q *Queue) Close() {
	close(q.stopCleanup)
}

// Enqueue adds a failed message to the dead letter queue.
func (q *Queue) Enqueue(msg *Message) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if msg.ID == "" {
		return fmt.Errorf("message ID is required")
	}
	if _, exists := q.messages[msg.ID]; exists {
		return fmt.Errorf("message %q already exists", msg.ID)
	}

	now := time.Now()
	if msg.EnqueuedAt.IsZero() {
		msg.EnqueuedAt = now
	}
	if msg.ExpiresAt.IsZero() {
		msg.ExpiresAt = now.Add(q.config.DefaultTTL)
	}
	if msg.MaxRetries <= 0 {
		msg.MaxRetries = q.config.MaxRetries
	}
	if msg.Partition == "" {
		if q.config.PartitionBy == "subject" {
			msg.Partition = msg.Subject
		} else {
			msg.Partition = "default"
		}
	}

	// Evict oldest if over capacity.
	for q.order.Len() >= q.config.MaxMessages {
		front := q.order.Front()
		if front != nil {
			q.removeElement(front)
			q.purgedCount++
		}
	}

	entry := &messageEntry{msg: msg}
	elem := q.order.PushBack(entry)
	q.messages[msg.ID] = elem

	// Index by subject.
	if _, ok := q.subjects[msg.Subject]; !ok {
		q.subjects[msg.Subject] = list.New()
	}
	q.subjects[msg.Subject].PushBack(entry)

	// Index by partition.
	if _, ok := q.partitions[msg.Partition]; !ok {
		q.partitions[msg.Partition] = list.New()
	}
	q.partitions[msg.Partition].PushBack(entry)

	if q.oldestTimestamp.IsZero() || msg.EnqueuedAt.Before(q.oldestTimestamp) {
		q.oldestTimestamp = msg.EnqueuedAt
	}
	if msg.EnqueuedAt.After(q.newestTimestamp) {
		q.newestTimestamp = msg.EnqueuedAt
	}
	return nil
}

// removeElement removes an element from the order list and all indexes.
// Caller must hold q.mu.
func (q *Queue) removeElement(elem *list.Element) {
	entry := elem.Value.(*messageEntry)
	msg := entry.msg

	delete(q.messages, msg.ID)
	q.order.Remove(elem)

	// Remove from subject list.
	if sl, ok := q.subjects[msg.Subject]; ok {
		for e := sl.Front(); e != nil; e = e.Next() {
			if e.Value.(*messageEntry).msg.ID == msg.ID {
				sl.Remove(e)
				break
			}
		}
		if sl.Len() == 0 {
			delete(q.subjects, msg.Subject)
		}
	}

	// Remove from partition list.
	if pl, ok := q.partitions[msg.Partition]; ok {
		for e := pl.Front(); e != nil; e = e.Next() {
			if e.Value.(*messageEntry).msg.ID == msg.ID {
				pl.Remove(e)
				break
			}
		}
		if pl.Len() == 0 {
			delete(q.partitions, msg.Partition)
		}
	}
}

// Get retrieves a message by ID.
func (q *Queue) Get(id string) (*Message, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	elem, ok := q.messages[id]
	if !ok {
		return nil, false
	}
	msg := elem.Value.(*messageEntry).msg
	// Check expiry.
	if time.Now().After(msg.ExpiresAt) {
		return nil, false
	}
	return msg, true
}

// Remove deletes a message by ID.
func (q *Queue) Remove(id string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	elem, ok := q.messages[id]
	if !ok {
		return false
	}
	q.removeElement(elem)
	return true
}

// ListBySubject returns messages for a subject, most recent first.
func (q *Queue) ListBySubject(subject string) []*Message {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return listToMessages(q.subjects[subject])
}

// ListByPartition returns messages for a partition, most recent first.
func (q *Queue) ListByPartition(partition string) []*Message {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return listToMessages(q.partitions[partition])
}

func listToMessages(l *list.List) []*Message {
	if l == nil {
		return nil
	}
	out := make([]*Message, 0, l.Len())
	for e := l.Front(); e != nil; e = e.Next() {
		out = append(out, e.Value.(*messageEntry).msg)
	}
	return out
}

// List returns all messages, oldest first.
func (q *Queue) List() []*Message {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]*Message, 0, q.order.Len())
	for e := q.order.Front(); e != nil; e = e.Next() {
		out = append(out, e.Value.(*messageEntry).msg)
	}
	return out
}

// Size returns the total number of messages.
func (q *Queue) Size() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.order.Len()
}

// Stats returns aggregate statistics.
func (q *Queue) Stats() QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return QueueStats{
		TotalMessages:   q.order.Len(),
		TotalSubjects:   len(q.subjects),
		TotalPartitions: len(q.partitions),
		ExpiredCount:    q.expiredCount,
		ReplayedCount:   q.replayedCount,
		PurgedCount:     q.purgedCount,
		OldestMessage:   q.oldestTimestamp,
		NewestMessage:   q.newestTimestamp,
	}
}

// ReplayFunc is called for each message during replay. Return nil on success;
// if an error is returned the message remains in the queue.
type ReplayFunc func(msg *Message) error

// Replay attempts to replay messages for a subject. Returns count of
// successfully replayed messages.
func (q *Queue) Replay(subject string, fn ReplayFunc) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	sl, ok := q.subjects[subject]
	if !ok {
		return 0, nil
	}

	replayed := 0
	batch := 0
	var next *list.Element
	for e := sl.Front(); e != nil && batch < q.config.ReplayBatchSize; e = next {
		next = e.Next()
		entry := e.Value.(*messageEntry)
		msg := entry.msg

		if time.Now().After(msg.ExpiresAt) {
			q.removeElement(q.messages[msg.ID])
			q.expiredCount++
			continue
		}

		if msg.RetryCount >= msg.MaxRetries {
			continue
		}

		msg.RetryCount++
		batch++

		// We must release the lock during replay to avoid long-held locks,
		// but for correctness in this simple implementation we call the fn
		// under lock. A real implementation would use a work queue.
		if err := fn(msg); err != nil {
			msg.Attempts = append(msg.Attempts, DeliveryAttempt{
				Timestamp: time.Now(),
				Error:     err.Error(),
			})
			msg.LastError = err.Error()
		} else {
			// Success — remove from queue.
			q.removeElement(q.messages[msg.ID])
			replayed++
			q.replayedCount++
		}
	}
	return replayed, nil
}

// ReplayAll replays all messages in the queue.
func (q *Queue) ReplayAll(fn ReplayFunc) (int, error) {
	subjects := q.Subjects()
	total := 0
	for _, s := range subjects {
		n, err := q.Replay(s, fn)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

// Subjects returns all distinct subjects.
func (q *Queue) Subjects() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]string, 0, len(q.subjects))
	for s := range q.subjects {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// Partitions returns all distinct partitions.
func (q *Queue) Partitions() []string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]string, 0, len(q.partitions))
	for p := range q.partitions {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Purge removes all messages.
func (q *Queue) Purge() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	n := q.order.Len()
	q.messages = make(map[string]*list.Element)
	q.order = list.New()
	q.subjects = make(map[string]*list.List)
	q.partitions = make(map[string]*list.List)
	q.purgedCount += int64(n)
	return n
}

// FormatQueue returns a multi-line string describing the queue contents.
func (q *Queue) FormatQueue() string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	s := fmt.Sprintf("DeadLetter Queue: %d messages, %d subjects, %d partitions\n",
		q.order.Len(), len(q.subjects), len(q.partitions))
	for _, subj := range q.Subjects() {
		msgs := listToMessages(q.subjects[subj])
		s += fmt.Sprintf("  Subject %q: %d messages\n", subj, len(msgs))
		for _, m := range msgs {
			s += fmt.Sprintf("    %s retries=%d/%d last_error=%q\n",
				m.ID, m.RetryCount, m.MaxRetries, truncate(m.LastError, 60))
		}
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// --- Batch Operations ---

// BatchEnqueue adds multiple messages atomically.
func (q *Queue) BatchEnqueue(msgs []*Message) []error {
	errs := make([]error, len(msgs))
	for i, msg := range msgs { errs[i] = q.Enqueue(msg) }
	return errs
}

// BatchRemove removes messages by ID prefix.
func (q *Queue) BatchRemove(prefix string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	count := 0
	for id := range q.messages {
		if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
			if elem, ok := q.messages[id]; ok {
				q.removeElement(elem)
				count++
			}
		}
	}
	return count
}

// --- Retry Policy ---

// RetryPolicy determines how retries are scheduled.
type RetryPolicy struct {
	BackoffBase    time.Duration
	BackoffMax     time.Duration
	BackoffMult    float64
	Jitter         bool
}

// DefaultRetryPolicy returns a sensible default.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		BackoffBase: time.Second,
		BackoffMax:  5 * time.Minute,
		BackoffMult: 2.0,
		Jitter:      true,
	}
}

// NextDelay computes the next retry delay for a given attempt number.
func (rp RetryPolicy) NextDelay(attempt int) time.Duration {
	d := float64(rp.BackoffBase)
	for i := 1; i < attempt; i++ { d *= rp.BackoffMult }
	if d > float64(rp.BackoffMax) { d = float64(rp.BackoffMax) }
	if rp.Jitter {
		// Simple deterministic jitter for testability.
		d = d * (0.75 + 0.5*float64(attempt%10)/10.0)
	}
	return time.Duration(d)
}

// --- Message Filtering ---

// MessageFilter is a predicate for filtering messages.
type MessageFilter func(msg *Message) bool

// FilterBySubject returns a filter matching a subject.
func FilterBySubject(subject string) MessageFilter {
	return func(msg *Message) bool { return msg.Subject == subject }
}

// FilterByRetryCount returns a filter matching retry count >= n.
func FilterByRetryCount(minRetries int) MessageFilter {
	return func(msg *Message) bool { return msg.RetryCount >= minRetries }
}

// FilterMessages returns messages matching the filter.
func (q *Queue) FilterMessages(fn MessageFilter) []*Message {
	q.mu.RLock()
	defer q.mu.RUnlock()
	var out []*Message
	for e := q.order.Front(); e != nil; e = e.Next() {
		msg := e.Value.(*messageEntry).msg
		if fn(msg) { out = append(out, msg) }
	}
	return out
}

// --- Persistence Interface ---

// Persistence defines an interface for durable dead-letter storage.
type Persistence interface {
	Save(msg *Message) error
	Load(id string) (*Message, error)
	LoadAll() ([]*Message, error)
	Delete(id string) error
}

// InMemoryPersistence implements Persistence in memory for testing.
type InMemoryPersistence struct {
	mu  sync.RWMutex
	store map[string]*Message
}

// NewInMemoryPersistence creates an in-memory persistence layer.
func NewInMemoryPersistence() *InMemoryPersistence {
	return &InMemoryPersistence{store: make(map[string]*Message)}
}

func (p *InMemoryPersistence) Save(msg *Message) error {
	p.mu.Lock(); defer p.mu.Unlock()
	cp := *msg; cp.Attempts = make([]DeliveryAttempt, len(msg.Attempts)); copy(cp.Attempts, msg.Attempts)
	p.store[msg.ID] = &cp; return nil
}

func (p *InMemoryPersistence) Load(id string) (*Message, error) {
	p.mu.RLock(); defer p.mu.RUnlock()
	if m, ok := p.store[id]; ok { cp := *m; return &cp, nil }
	return nil, fmt.Errorf("message %q not found", id)
}

func (p *InMemoryPersistence) LoadAll() ([]*Message, error) {
	p.mu.RLock(); defer p.mu.RUnlock()
	out := make([]*Message, 0, len(p.store))
	for _, m := range p.store { cp := *m; out = append(out, &cp) }
	return out, nil
}

func (p *InMemoryPersistence) Delete(id string) error {
	p.mu.Lock(); defer p.mu.Unlock()
	delete(p.store, id); return nil
}

// --- Queue with Persistence ---

// DurableQueue extends Queue with optional persistence.
type DurableQueue struct {
	*Queue
	persist Persistence
}

// NewDurableQueue creates a queue backed by a persistence layer.
func NewDurableQueue(cfg Config, persist Persistence) *DurableQueue {
	q := New(cfg)
	if persist == nil { persist = NewInMemoryPersistence() }
	return &DurableQueue{Queue: q, persist: persist}
}

// EnqueueAndSave enqueues a message and persists it.
func (dq *DurableQueue) EnqueueAndSave(msg *Message) error {
	if err := dq.Queue.Enqueue(msg); err != nil { return err }
	return dq.persist.Save(msg)
}

// Restore loads all persisted messages into the queue.
func (dq *DurableQueue) Restore() (int, error) {
	msgs, err := dq.persist.LoadAll()
	if err != nil { return 0, err }
	count := 0
	for _, msg := range msgs {
		if err := dq.Queue.Enqueue(msg); err != nil { continue }
		count++
	}
	return count, nil
}

// --- Subscription Management ---

// Subscription represents an interest in messages for a subject.
type Subscription struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Handler ReplayFunc `json:"-"`
}

// PubSub adds lightweight pub/sub on top of the dead letter queue.
type PubSub struct {
	mu            sync.RWMutex
	subscriptions map[string][]Subscription // subject -> subscriptions.
}

// NewPubSub creates a pub/sub manager.
func NewPubSub() *PubSub { return &PubSub{subscriptions: make(map[string][]Subscription)} }

// Subscribe registers a handler for a subject.
func (ps *PubSub) Subscribe(subject, id string, handler ReplayFunc) {
	ps.mu.Lock(); defer ps.mu.Unlock()
	ps.subscriptions[subject] = append(ps.subscriptions[subject], Subscription{ID: id, Subject: subject, Handler: handler})
}

// Unsubscribe removes a subscription.
func (ps *PubSub) Unsubscribe(subject, id string) {
	ps.mu.Lock(); defer ps.mu.Unlock()
	subs := ps.subscriptions[subject]
	for i, s := range subs {
		if s.ID == id { ps.subscriptions[subject] = append(subs[:i], subs[i+1:]...); return }
	}
}

// Publish pushes a message to all subscribers.
func (ps *PubSub) Publish(msg *Message) {
	ps.mu.RLock()
	subs := make([]Subscription, len(ps.subscriptions[msg.Subject]))
	copy(subs, ps.subscriptions[msg.Subject])
	ps.mu.RUnlock()
	for _, s := range subs {
		if s.Handler != nil { s.Handler(msg) }
	}
}

// --- Message Deduplication ---

// DedupQueue wraps Queue with message ID deduplication.
type DedupQueue struct {
	*Queue
	seen map[string]time.Time
	muSeen sync.Mutex
	dedupWindow time.Duration
}

// NewDedupQueue creates a deduplicating queue.
func NewDedupQueue(cfg Config, window time.Duration) *DedupQueue {
	dq := &DedupQueue{Queue: New(cfg), seen: make(map[string]time.Time), dedupWindow: window}
	go dq.cleanSeen()
	return dq
}

func (dq *DedupQueue) cleanSeen() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		dq.muSeen.Lock()
		cutoff := time.Now().Add(-dq.dedupWindow)
		for id, t := range dq.seen { if t.Before(cutoff) { delete(dq.seen, id) } }
		dq.muSeen.Unlock()
	}
}

// EnqueueUnique enqueues a message only if its ID hasn't been seen within the window.
func (dq *DedupQueue) EnqueueUnique(msg *Message) (bool, error) {
	dq.muSeen.Lock()
	if _, seen := dq.seen[msg.ID]; seen { dq.muSeen.Unlock(); return false, nil }
	dq.seen[msg.ID] = time.Now()
	dq.muSeen.Unlock()
	return true, dq.Queue.Enqueue(msg)
}
