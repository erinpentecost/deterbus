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

type Callback[T any] func(context.Context, T)
type Unsubscribe func()

func (t *Topic[T]) Subscribe(callback Callback[T]) Unsubscribe {
	// increment id
	id := t.nextCallbackID.Add(1)

	cc := &callbackContainer[T]{
		id: id,
		fn: callback,
	}

	// publish callback sub event
	t.bus.publish(&subscribeEvent[T]{
		topic:    t,
		callback: cc,
	})

	// unsubscribe func
	return func() {
		t.bus.publish(&unsubscribeEvent[T]{
			topic:      t,
			callbackID: id,
		})
	}
}
