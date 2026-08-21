package weavert

import (
	"sync"
	"testing"
	"time"
)

// newBlockingActor returns an actor whose "block" handler blocks until
// gate is closed, then replies true — used to prove several actors are
// genuinely running at once, not queued behind one another.
func newBlockingActor(gate <-chan struct{}, arrived chan<- struct{}) any {
	o := NewObject()
	ObjSet(o, "block", wrapMethod(func(self, replyTo any) any {
		arrived <- struct{}{}
		<-gate
		Reply(replyTo, true)
		return nil
	}))
	return o
}

// TestActors_RunConcurrently proves weave_spec.md §6.4's "異なるアクター
// 同士は完全に並行に動く": N actors each block inside their handler
// until every one of them has entered it. If actors were processed one
// at a time (e.g. accidentally sharing a single goroutine/mutex instead
// of each having its own), this deadlocks and the test times out — it
// can only succeed if all N goroutines are genuinely running at once.
func TestActors_RunConcurrently(t *testing.T) {
	const n = 8
	gate := make(chan struct{})
	arrived := make(chan struct{}, n)

	var wg sync.WaitGroup
	results := make([]any, n)
	for i := 0; i < n; i++ {
		a := Spawn(newBlockingActor(gate, arrived))
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = Call(Ask(a), "block")
		}(i)
	}

	// Wait for all n actors to have reached the blocking point. If any
	// were serialized behind another, this times out.
	for i := 0; i < n; i++ {
		select {
		case <-arrived:
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/%d actors reached the handler concurrently — actors are not running in parallel", i, n)
		}
	}

	close(gate) // release every actor at once
	wg.Wait()

	for i, r := range results {
		if r != true {
			t.Errorf("actor %d result = %v, want true", i, r)
		}
	}
}

// TestActor_ProcessesOwnMessagesSequentially proves weave_spec.md §6.4's
// "1つのアクターは...常に1つずつ順番に処理する": many goroutines send
// `increment` concurrently to the SAME actor; since state mutation
// (ObjGet/ObjSet on a plain Go map, with no locking of its own) would
// race if two messages were ever handled at the same time, a correct
// final count is only possible if the actor truly serializes its inbox
// — this is exactly what `go test -race` is positioned to also catch.
func TestActor_ProcessesOwnMessagesSequentially(t *testing.T) {
	a := Spawn(newTestCounter())

	const goroutines = 50
	const perGoroutine = 20
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perGoroutine; j++ {
				Call(Call(Send(a), "increment"), 1.0)
			}
		}()
	}
	wg.Wait()

	got := Call(Ask(a), "get")
	want := float64(goroutines * perGoroutine)
	if got != want {
		t.Errorf("count = %v, want %v (a lower count means messages were lost or interleaved unsafely)", got, want)
	}
}
