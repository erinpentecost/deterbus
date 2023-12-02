package bus

import (
	"context"
	"sync"
	"sync/atomic"
)

// Topic is what users interact with.
// Each one watches the station to know when it must run.
type Topic[T any] struct {
	bus *Bus

	nextCallbackID *atomic.Uint64

	// newer callbacks must go on the end
	callbackLock *sync.Mutex
	callbacks    []*callbackContainer[T]
}

// NewTopic creates a new topic on an event bus.
func NewTopic[T any](bus *Bus) *Topic[T] {
	t := &Topic[T]{
		bus:            bus,
		nextCallbackID: &atomic.Uint64{},
		callbackLock:   &sync.Mutex{},
		callbacks:      []*callbackContainer[T]{},
	}
	return t
}

// Waiter can be used to signal when all Subscribers have finished
// processing an event.
type Waiter interface {
	Wait()
}

// Publish submits an event to queue. When processed, it will be sent
// to every Callback that is Subscribed to this Topic.
func (t *Topic[T]) Publish(ctx context.Context, arg T) Waiter {
	wg := sync.WaitGroup{}
	wg.Add(1)
	ev := &event[T]{
		ctxArg: ctx,
		arg:    arg,
		topic:  t,
		wg:     &wg,
	}

	t.bus.publish(ev)

	return &wg
}

// Callbacks are hooked up to Topics via Subscription.
// They are called for each Event that is Published to the Topic.
type Callback[T any] func(context.Context, T)

// CallbackManager contains utility functions for managing a callback.
type CallbackManager interface {
	// Wait will block until the publish is queued.
	Wait()
	// Unsubscribe will queue an event to unsubscribe the callback.
	// The Wait function returned by Unsubscribe will block until that
	// removal event is queued.
	//
	// Note that the callback may still be invoked even after
	// CallbackManager.Unsubscribe()() is called if there is a backlog for
	// this topic.
	Unsubscribe() (Wait func())
}

type callbackManager struct {
	wait        func()
	unsubscribe func() func()
}

func (c *callbackManager) Wait() {
	c.wait()
}

func (c *callbackManager) Unsubscribe() (Wait func()) {
	return c.unsubscribe()
}

// Subscribe will invoke callback for each event Published to this topic.
func (t *Topic[T]) Subscribe(callback Callback[T]) CallbackManager {
	// increment id
	id := t.nextCallbackID.Add(1)

	cc := &callbackContainer[T]{
		id: id,
		fn: callback,
	}

	// publish callback sub event
	pubDone := &sync.WaitGroup{}
	pubDone.Add(1)
	t.bus.publish(&subscribeEvent[T]{
		topic:    t,
		callback: cc,
		wg:       pubDone,
	})

	// unsubscribe func
	unsub := func() func() {
		unsubDone := &sync.WaitGroup{}
		unsubDone.Add(1)
		t.bus.publish(&unsubscribeEvent[T]{
			topic:      t,
			callbackID: id,
			wg:         unsubDone,
		})
		return unsubDone.Wait
	}

	return &callbackManager{wait: pubDone.Wait, unsubscribe: unsub}
}
