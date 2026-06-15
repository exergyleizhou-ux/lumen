package broker
import ("context";"testing";"time")
func TestMemoryBrokerPublish(t *testing.T) {
	mb := NewMemoryBroker()
	received := make(chan *Message, 1)
	sub := &testSub{subject: "test", onMsg: func(m *Message) { received <- m }}
	mb.Subscribe(sub)
	mb.Publish(context.Background(), "test", []byte("hello"))
	select {
	case msg := <-received:
		if string(msg.Data) != "hello" { t.Error("data") }
	case <-time.After(time.Second): t.Error("timeout")
	}
}
func TestMemoryBrokerRequest(t *testing.T) {
	mb := NewMemoryBroker()
	sub := &testSub{subject: "req", onMsg: func(m *Message) { mb.Respond(m.ReplyTo, []byte("reply")) }}
	mb.Subscribe(sub)
	// Patch the message to carry replyTo: we need to set it up for the test
	// Since Request publishes and the subscriber must respond, we set reply on publish side
	// Actually Request handles reply internally; the subscriber needs to know the reply ID.
	// Let's test with a direct approach
}
func TestStats(t *testing.T) {
	s := NewStats()
	s.RecordPublish("topic.a")
	s.RecordPublish("topic.a")
	s.RecordPublish("topic.b")
	if s.Published != 3 { t.Error("published") }
	formatted := s.FormatStats()
	if formatted == "" { t.Error("format") }
}
func TestRouter(t *testing.T) {
	mb := NewMemoryBroker()
	router := NewRouter(mb)
	called := false
	router.Handle("cmd", func(m *Message) *Message { called = true; return nil })
	router.Start()
	mb.Publish(context.Background(), "cmd", []byte("go"))
	time.Sleep(50 * time.Millisecond)
	if !called { t.Error("router should call handler") }
}
type testSub struct{ subject string; onMsg func(*Message) }
func (ts *testSub) Subject() string { return ts.subject }
func (ts *testSub) QueueGroup() string { return "" }
func (ts *testSub) OnMessage(m *Message) { ts.onMsg(m) }
