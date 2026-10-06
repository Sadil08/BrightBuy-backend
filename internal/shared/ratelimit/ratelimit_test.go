package ratelimit

import "testing"

func TestAllowPermitsBurstThenRejects(t *testing.T) {
	// A very slow refill rate (effectively never, for the duration of this test) with burst 3 — so
	// the 4th call in quick succession must be rejected deterministically, no timing flakiness.
	l := New(0.0001, 3)

	for i := 0; i < 3; i++ {
		if !l.Allow("key-a") {
			t.Fatalf("call %d: Allow returned false, want true (within burst)", i+1)
		}
	}
	if l.Allow("key-a") {
		t.Error("4th call: Allow returned true, want false (burst exhausted)")
	}
}

func TestAllowTracksKeysIndependently(t *testing.T) {
	l := New(0.0001, 1)

	if !l.Allow("key-a") {
		t.Fatal("first call for key-a should be allowed")
	}
	if l.Allow("key-a") {
		t.Error("second call for key-a should be rejected (burst 1, already used)")
	}
	if !l.Allow("key-b") {
		t.Error("first call for key-b should be allowed — a different key has its own bucket")
	}
}
