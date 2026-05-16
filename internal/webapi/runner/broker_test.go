package runner_test

import (
	"testing"
	"time"

	"github.com/klehmer/nimbusfab/internal/webapi/runner"
	"github.com/klehmer/nimbusfab/pkg/provisioner"
)

func TestBroker_SubscribeReceives(t *testing.T) {
	b := runner.NewRunBroker(16)
	ch, unsub := b.Subscribe("d-1")
	defer unsub()
	pub := b.Publisher("d-1")

	go func() {
		pub <- provisioner.RunEvent{Kind: provisioner.RunEventStart, Message: "a"}
		pub <- provisioner.RunEvent{Kind: provisioner.RunEventLog, Message: "b"}
		pub <- provisioner.RunEvent{Kind: provisioner.RunEventSuccess, Message: "c"}
		close(pub)
	}()

	got := []string{}
	for evt := range ch {
		got = append(got, evt.Message)
	}
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("events = %v, want [a b c]", got)
	}
}

func TestBroker_MultipleSubscribers(t *testing.T) {
	b := runner.NewRunBroker(16)
	ch1, unsub1 := b.Subscribe("d-1")
	defer unsub1()
	ch2, unsub2 := b.Subscribe("d-1")
	defer unsub2()
	pub := b.Publisher("d-1")

	go func() {
		pub <- provisioner.RunEvent{Message: "x"}
		close(pub)
	}()

	for _, ch := range []<-chan provisioner.RunEvent{ch1, ch2} {
		evt, ok := <-ch
		if !ok {
			t.Fatal("subscriber received no event before close")
		}
		if evt.Message != "x" {
			t.Errorf("evt.Message = %q", evt.Message)
		}
	}
}

func TestBroker_DifferentDeploymentsIsolated(t *testing.T) {
	b := runner.NewRunBroker(16)
	chA, unsubA := b.Subscribe("d-A")
	defer unsubA()
	pubB := b.Publisher("d-B")

	go func() {
		pubB <- provisioner.RunEvent{Message: "B-only"}
		close(pubB)
	}()

	select {
	case evt, ok := <-chA:
		if ok {
			t.Errorf("subscriber A received event for B: %v", evt)
		}
	case <-time.After(100 * time.Millisecond):
		// Expected: no events arrived.
	}
}

func TestBroker_PublisherCloseSignalsSubscribers(t *testing.T) {
	b := runner.NewRunBroker(16)
	ch, unsub := b.Subscribe("d-1")
	defer unsub()
	pub := b.Publisher("d-1")
	close(pub)
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected ch to close, but received event")
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("subscriber channel did not close within 200ms")
	}
}

func TestBroker_LateSubscriberMissesEarlyEvents(t *testing.T) {
	b := runner.NewRunBroker(16)
	pub := b.Publisher("d-1")
	pub <- provisioner.RunEvent{Message: "missed"}
	// Give the publisher goroutine a moment to dispatch.
	time.Sleep(20 * time.Millisecond)

	ch, unsub := b.Subscribe("d-1")
	defer unsub()

	pub <- provisioner.RunEvent{Message: "seen"}
	close(pub)

	got := []string{}
	for evt := range ch {
		got = append(got, evt.Message)
	}
	if len(got) != 1 || got[0] != "seen" {
		t.Errorf("got = %v, want [seen]", got)
	}
}

func TestBroker_Unsubscribe(t *testing.T) {
	// unsubscribe only removes the channel from the dispatch list; it
	// does NOT close the channel (publisher close owns that). So after
	// unsub, the channel never receives further events but also never
	// closes from our side. Drain "first", then verify no more events
	// arrive via a timed select.
	b := runner.NewRunBroker(16)
	ch, unsub := b.Subscribe("d-1")
	pub := b.Publisher("d-1")
	defer close(pub) // close to clean up goroutine; ch is gone from list so no double-close

	pub <- provisioner.RunEvent{Message: "first"}
	// Wait for "first" to arrive.
	select {
	case evt := <-ch:
		if evt.Message != "first" {
			t.Errorf("first event message = %q", evt.Message)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("did not receive first event in time")
	}
	unsub()

	pub <- provisioner.RunEvent{Message: "after-unsub"}
	select {
	case evt, ok := <-ch:
		if ok {
			t.Errorf("received event after unsubscribe: %v", evt)
		}
	case <-time.After(100 * time.Millisecond):
		// Expected: no event arrives because dispatch list no longer
		// contains our channel.
	}
}
