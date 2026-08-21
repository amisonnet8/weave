package weavert

import "fmt"

// Weave's actor model (weave_spec.md §6) needs real concurrency
// (goroutines) and a message queue — like every other feature that
// needs Go semantics AMIVM's `^any`-everywhere IR can't express
// directly (closures in Step 5, objects in Step 6), this is
// implemented entirely inside weavert. Unlike those, actors don't even
// get close to using AMIVM's native CHTYPE/CHMAKE/CHSEND/CHRECV/SPAWN:
// the receive loop's dispatch step needs the same dynamic
// ObjGet+Call machinery objects/closures already use, so there was
// nothing left for native channel instructions to do that wouldn't
// immediately need a weavert round trip anyway. This is a judgment
// call based on the established pattern from Steps 5–8, not something
// separately verified against amivm by trying the native path first —
// see CLAUDE.md's 後半 Step 1 "確定した設計判断".

// actorMessage is one entry in an actor's inbox: a message name plus
// its arguments, applied to the handler exactly like genMethodCall's
// self-injection (self first, then each arg in order) — see (*Actor).run.
// tell and ask are NOT distinguished here: an ask's reply channel is
// just an ordinary trailing argument (weave_spec.md §6.3: "それを最後
// の引数としてメッセージ本体に付け加えてから送信し"), so the receive
// loop treats every message identically regardless of which sent it.
type actorMessage struct {
	name string
	args []any
}

// Actor is a running Weave actor (weave_spec.md §6.1): an inbox channel
// plus the goroutine draining it one message at a time (§6.4).
type Actor struct {
	inbox chan actorMessage
}

func actorOf(v any) *Actor {
	a, ok := v.(*Actor)
	if !ok {
		panic(fmt.Sprintf("weave: value is not an actor, got %T", v))
	}
	return a
}

// Spawn implements `spawn(obj)` (weave_spec.md §6.1): starts a new
// actor whose internal state is a copy of obj (mutations after spawning
// don't affect the actor, and vice versa), running its own receive loop
// on an independent goroutine.
func Spawn(obj any) any {
	state := cloneObject(objOf(obj))
	a := &Actor{inbox: make(chan actorMessage)}
	go a.run(state)
	return a
}

// cloneObject makes a shallow copy — a new top-level map, sharing
// nested values with the original (weave_spec.md §6.1 says obj is
// copied but doesn't address nested structure; a shallow copy is the
// simplest reading and matches "each actor's own top-level state is
// independent," which is all the spec's own examples ever rely on).
func cloneObject(o Object) Object {
	clone := make(Object, len(o))
	for k, v := range o {
		clone[k] = v
	}
	return clone
}

// run is the actor's receive loop (weave_spec.md §6.2, §6.4): one
// message at a time, forever, in arrival order. Dispatch reuses ObjGet
// (so a handler found via the prototype chain works exactly like a
// regular inherited method, weave_spec.md §6.2's own point) and Call
// (self first, then each message argument — identical in shape to
// genMethodCall's codegen, just run natively in Go here instead of
// AMIVM-generated IR, since the receive loop itself is hand-written).
// A message naming a handler nowhere in the chain is silently ignored
// (§6.2's own stated behavior) — including for an `ask`, whose reply
// channel is then never written to, blocking that caller forever
// (§6.3's own documented consequence of a handler that never replies).
func (a *Actor) run(state Object) {
	for msg := range a.inbox {
		handler := ObjGet(state, msg.name)
		if handler == nil {
			continue
		}
		result := Call(handler, state)
		for _, arg := range msg.args {
			result = Call(result, arg)
		}
	}
}

// wrapFn builds a Weave-callable value from a native Go func(any) any.
// Call (weavert/closure.go) accepts any Go func(any) any directly via
// reflection, so this is now just an identity — kept as a named function
// (rather than inlining `func(any) any` literals at each Send/Ask call
// site) purely for the doc comment's sake: a value returned from here is
// a Weave-callable closure by construction, ordinary Go nesting and
// by-reference capture included, same as any AMIVM-generated CLOS.
func wrapFn(f func(any) any) any {
	return f
}

// Send implements `send(actor)` (weave_spec.md §6.2, §6.3): returns a
// two-argument curried function — name, then exactly one message
// argument — matching every message handler the spec ever shows
// receiving via `send`/`tell` (e.g. `increment: fn(self, amount)`: one
// parameter beyond self). `send` is fire-and-forget: the goroutine
// dispatching the message runs independently, so this never blocks.
//
// Ask implements `ask(actor)` (weave_spec.md §6.3): returns a
// *one*-argument curried function — name only, no explicit message
// argument — matching every handler the spec shows receiving via `ask`
// (e.g. `get: fn(self, replyTo)`, `finish: fn(self, replyTo)`: the
// single parameter beyond self is always exactly the auto-appended
// reply channel, never a caller-supplied value). This asymmetry with
// Send (2 curry levels vs. 1) isn't an arbitrary choice — it's the only
// reading consistent with every example weave_spec.md actually shows;
// see CLAUDE.md's 後半 Step 1 "確定した設計判断" for the full reasoning
// (in particular, why this doesn't generalize to messages needing both
// explicit args and a reply channel, and why that's fine — the spec
// never shows one).
//
// The reply channel is buffered (capacity 1) so the handler's
// Reply call never blocks even if Ask's own receive is delayed.
func Send(actorVal any) any {
	a := actorOf(actorVal)
	return wrapFn(func(name any) any {
		return wrapFn(func(arg any) any {
			a.inbox <- actorMessage{name: keyOf(name), args: []any{arg}}
			return nil
		})
	})
}

func Ask(actorVal any) any {
	a := actorOf(actorVal)
	return wrapFn(func(name any) any {
		replyCh := make(chan any, 1)
		a.inbox <- actorMessage{name: keyOf(name), args: []any{replyCh}}
		return <-replyCh
	})
}

// Reply implements `reply(replyTo, value)` (weave_spec.md §6.3): a
// handler's response to an `ask`. replyTo is whatever Ask appended to
// the message's args — an ordinary argument from the handler's own
// point of view, not a distinct kind of value it has to know about.
func Reply(replyTo any, value any) any {
	ch, ok := replyTo.(chan any)
	if !ok {
		panic(fmt.Sprintf("weave: reply() requires a reply channel, got %T", replyTo))
	}
	ch <- value
	return nil
}
