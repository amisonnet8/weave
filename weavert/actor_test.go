package weavert

import "testing"

func newTestCounter() any {
	o := NewObject()
	ObjSet(o, "count", 0.0)
	ObjSet(o, "increment", wrapMethod(func(self any, amount any) any {
		ObjSet(self, "count", ObjGet(self, "count").(float64)+amount.(float64))
		return nil
	}))
	ObjSet(o, "get", wrapMethod(func(self any, replyTo any) any {
		Reply(replyTo, ObjGet(self, "count"))
		return nil
	}))
	return o
}

// wrapMethod builds a Weave-callable two-argument (self, arg) method
// value out of a native Go func, mirroring how a compiled `fn(self, x)
// {...}` literal behaves at the weavert.Call boundary — for tests that
// need a handler without going through the full parser/codegen pipeline.
func wrapMethod(f func(self, arg any) any) any {
	return wrapFn(func(self any) any {
		return wrapFn(func(arg any) any {
			return f(self, arg)
		})
	})
}

func TestSpawnAndSend_IncrementsSequentially(t *testing.T) {
	a := Spawn(newTestCounter())
	Call(Call(Send(a), "increment"), 5.0)
	Call(Call(Send(a), "increment"), 3.0)
	got := Call(Ask(a), "get")
	if got != 8.0 {
		t.Errorf("count after two increments = %v, want 8", got)
	}
}

func TestAsk_ReturnsHandlerReply(t *testing.T) {
	a := Spawn(newTestCounter())
	got := Call(Ask(a), "get")
	if got != 0.0 {
		t.Errorf("initial count = %v, want 0", got)
	}
}

func TestSend_UnknownMessageIsSilentlyIgnored(t *testing.T) {
	a := Spawn(newTestCounter())
	Call(Call(Send(a), "noSuchMessage"), 1.0)
	// If the actor's loop panicked or got stuck on the unknown message,
	// a subsequent ask would never return (weave_spec.md §6.4: messages
	// are processed strictly in order).
	got := Call(Ask(a), "get")
	if got != 0.0 {
		t.Errorf("count after an unknown message = %v, want unchanged 0", got)
	}
}

func TestSpawn_CopiesObjectNotReference(t *testing.T) {
	obj := newTestCounter()
	a := Spawn(obj)
	ObjSet(obj, "count", 999.0) // mutate the original after spawning
	got := Call(Ask(a), "get")
	if got != 0.0 {
		t.Errorf("count = %v, want 0 (actor state must be independent of the original object)", got)
	}
}

func TestSpawn_PrototypeChainWorksForMessageDispatch(t *testing.T) {
	base := NewObject()
	ObjSet(base, "greet", wrapMethod(func(self, replyTo any) any {
		Reply(replyTo, "hi "+ObjGet(self, "name").(string))
		return nil
	}))
	child := NewObject()
	ObjSet(child, protoKey, base)
	ObjSet(child, "name", "Bob")

	a := Spawn(child)
	got := Call(Ask(a), "greet")
	if got != "hi Bob" {
		t.Errorf("greet reply = %v, want %q", got, "hi Bob")
	}
}

func TestActorOf_NonActorPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Send on a non-actor to panic")
		}
	}()
	Send(5.0)
}

func TestReply_NonChannelPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected Reply with a non-channel to panic")
		}
	}()
	Reply(5.0, "x")
}
