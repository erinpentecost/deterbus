package bus

import (
	"context"
	"math"
	"sync"
	"sync/atomic"

	"github.com/eapache/queue"
)

type eventID uint64

// Bus just controls when topic handlers are invoked.
type Bus struct {
	// consumer syncs
	lastCompletedEventCond *sync.Cond
	lastCompletedEvent     eventID

	nextEventID *atomic.Uint64
}

func New() *Bus {
	b := &Bus{
		lastCompletedEventCond: sync.NewCond(&sync.Mutex{}),
		nextEventID:            &atomic.Uint64{},
	}

	return b
}

func NewTopic[T any](bus *Bus) *Topic[T] {
	t := &Topic[T]{
		bus:                  bus,
		pendingEvents:        queue.New(),
		oldestPendingEventID: eventID(math.MaxUint64),
	}

	// start consumer multiplexer
	// TODO: stop this goroutine from leaking
	go t.multiplexEventsToCallbacks()

	return t
}

type Callback[T any] func(context.Context, T)
type Unsubscribe func()

type callbackContainer[T any] struct {
	handlerID uint64
	fn        Callback[T]
}

type event[T any] struct {
	ctxArg context.Context
	arg    T
	id     eventID
}

// Topic is what users interact with.
// Each one watches the station to know when it must run.
type Topic[T any] struct {
	bus *Bus

	queueLock            sync.Mutex
	pendingEvents        *queue.Queue
	oldestPendingEventID eventID

	// newer callbacks must go on the end
	callbackLock   sync.Mutex
	callbacks      []callbackContainer[T]
	nextCallbackID uint64
}

func (t *Topic[T]) multiplexEventsToCallbacks() {
	for {
		// this wont ever unlock because we only broadcast this in one spot
		// we need to also trigger if oldestPendingEventID changes
		t.bus.lastCompletedEventCond.L.Lock()
		for t.bus.lastCompletedEvent != t.oldestPendingEventID-1 {
			t.bus.lastCompletedEventCond.Wait()
		}

		// pop event
		t.queueLock.Lock()
		event := t.pendingEvents.Remove().(event[T])

		t.updateOldest()
		t.queueLock.Unlock()

		// multiplex to callbacks here
		for _, cc := range t.callbacks {
			cc.fn(event.ctxArg, event.arg)
		}

		// let everyone know the id increased
		t.bus.lastCompletedEvent++
		t.bus.lastCompletedEventCond.Broadcast()

		t.bus.lastCompletedEventCond.L.Unlock()
	}
}

type Waiter interface {
	Wait()
}

type publishWaiter[T any] struct {
	topic     *Topic[T]
	waitForID eventID
}

func (p *publishWaiter[T]) Wait() {
	p.topic.bus.lastCompletedEventCond.L.Lock()
	for p.topic.bus.lastCompletedEvent < p.waitForID {
		p.topic.bus.lastCompletedEventCond.Wait()
	}
	p.topic.bus.lastCompletedEventCond.L.Unlock()
}

func (t *Topic[T]) Publish(ctx context.Context, arg T) Waiter {
	// reserve event id
	id := eventID(t.bus.nextEventID.Add(1))

	// lock queue and place event in it
	t.queueLock.Lock()
	t.pendingEvents.Add(event[T]{
		ctxArg: ctx,
		arg:    arg,
		id:     id,
	})
	t.updateOldest()
	t.queueLock.Unlock()

	return &publishWaiter[T]{topic: t, waitForID: id}
}

// updateOldest updates t.oldestPendingEventID.
// make sure you lock t.queueLock while calling
func (t *Topic[T]) updateOldest() {
	if t.pendingEvents.Length() == 0 {
		return
	}
	event := t.pendingEvents.Peek().(event[T])
	t.oldestPendingEventID = event.id
}

func (t *Topic[T]) Subscribe(callback Callback[T]) Unsubscribe {
	t.callbackLock.Lock()
	defer t.callbackLock.Unlock()
	// increment id
	id := t.nextCallbackID
	t.nextCallbackID += 1

	cc := callbackContainer[T]{
		handlerID: id,
		fn:        callback,
	}

	t.callbacks = append(t.callbacks, cc)

	// unsubscribe func
	return func() {
		t.callbackLock.Lock()
		defer t.callbackLock.Unlock()

		// Find the index for the handler.
		index := -1
		for i, handler := range t.callbacks {
			if handler.handlerID == id {
				index = i
				break
			}
		}

		if index < 0 {
			// not found
			return
		}

		// it's the last one
		if len(t.callbacks) == 1 {
			t.callbacks = []callbackContainer[T]{}
			return
		}

		// delete it
		t.callbacks = append(t.callbacks[:index], t.callbacks[index+1:]...)
	}
}
