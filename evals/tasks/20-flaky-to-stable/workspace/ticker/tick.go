package ticker

import "time"

type Ticker struct {
	C    <-chan time.Time
	done chan struct{}
}

func NewTicker(d time.Duration) *Ticker {
	ch := make(chan time.Time)
	done := make(chan struct{})
	// BUG: uses time.Sleep in goroutine — flaky
	go func() {
		for {
			select {
			case <-done:
				return
			default:
				time.Sleep(d)
				ch <- time.Now()
			}
		}
	}()
	return &Ticker{C: ch, done: done}
}

func (t *Ticker) Stop() {
	close(t.done)
}
