package eventbus

import (
	"testing"
	"time"
)

func TestPubSub(t *testing.T) {
	b := New(4)
	defer b.Close()
	s1 := b.Subscribe()
	defer s1.Cancel()
	s2 := b.Subscribe()
	defer s2.Cancel()

	go b.Publish(Event{Topic: "hello", AccountID: 1, Payload: "world"})

	for _, sub := range []*Subscriber{s1, s2} {
		select {
		case ev := <-sub.C:
			if ev.Topic != "hello" || ev.AccountID != 1 {
				t.Fatalf("unexpected event: %+v", ev)
			}
			if ev.At.IsZero() {
				t.Fatal("publisher should stamp At")
			}
		case <-time.After(1 * time.Second):
			t.Fatal("subscriber did not receive event")
		}
	}
}

func TestCancelUnsubscribes(t *testing.T) {
	b := New(2)
	defer b.Close()
	s := b.Subscribe()
	s.Cancel()

	b.Publish(Event{Topic: "x"})
	select {
	case _, ok := <-s.C:
		if ok {
			t.Fatal("event delivered after cancel")
		}
	case <-time.After(200 * time.Millisecond):
		// Channel closed by cancel; closed channel reads non-blocking with ok=false.
		// If we hit timeout, the channel never closed; that's a bug too.
		t.Fatal("channel did not close on cancel")
	}
}

func TestSlowSubscriberDoesNotBlockPublisher(t *testing.T) {
	b := New(2)
	defer b.Close()
	slow := b.Subscribe()
	defer slow.Cancel()
	fast := b.Subscribe()
	defer fast.Cancel()

	// fill slow buffer
	b.Publish(Event{Topic: "1"})
	b.Publish(Event{Topic: "2"})
	// next publish must be dropped for slow, delivered to fast.
	done := make(chan struct{})
	go func() {
		b.Publish(Event{Topic: "3"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("publish blocked on slow subscriber")
	}
	select {
	case ev := <-fast.C:
		if ev.Topic == "" {
			t.Fatal("fast got empty event")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("fast did not receive event")
	}
}
