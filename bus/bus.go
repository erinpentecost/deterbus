package bus

import (
	"context"
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
		bus:           bus,
		pendingEvents: queue.New(),
	}

	// start consumer multiplexer
	// TODO: it would be better if there was just single event channel
	// on the bus. that's messed up because of generics limitations
	// (if I could have a queue[T] of mixed Ts this would be easy,
	// but I also need to un-mix them and call the callbacks without reflection,
	// because reflection is slow. pick your poison)
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

	queueLock     sync.Mutex
	pendingEvents *queue.Queue

	// newer callbacks must go on the end
	callbackLock   sync.Mutex
	callbacks      []callbackContainer[T]
	nextCallbackID uint64
}

func (t *Topic[T]) multiplexEventsToCallbacks() {
	for {
		t.bus.lastCompletedEventCond.L.Lock()
		for !t.shouldRun() {
			t.bus.lastCompletedEventCond.Wait()
		}
		t.bus.lastCompletedEventCond.L.Unlock()

		// pop event
		t.queueLock.Lock()
		event := t.pendingEvents.Remove().(event[T])
		t.queueLock.Unlock()

		// multiplex to callbacks here
		// don't hold a lock on t.bus.lastCompletedEventCond.L here
		// because these callbacks may be Wait()ing on publishes.
		for _, cc := range t.callbacks {
			cc.fn(event.ctxArg, event.arg)
		}

		// let everyone know the id increased
		t.bus.lastCompletedEventCond.L.Lock()
		t.bus.lastCompletedEvent = event.id
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
	// lock queue and place event in it
	t.queueLock.Lock()

	// reserve event id
	id := eventID(t.bus.nextEventID.Add(1))

	t.pendingEvents.Add(event[T]{
		ctxArg: ctx,
		arg:    arg,
		id:     id,
	})
	t.queueLock.Unlock()

	// this is not ideal because it wakes up all topics, when it could
	// just wake up this one.
	t.bus.lastCompletedEventCond.Broadcast()

	return &publishWaiter[T]{topic: t, waitForID: id}
}

func (t *Topic[T]) oldestPendingEventID() *eventID {
	t.queueLock.Lock()
	defer t.queueLock.Unlock()
	if t.pendingEvents.Length() == 0 {
		return nil
	}
	id := t.pendingEvents.Peek().(event[T]).id
	return &id
}

// hold lastCompletedEventCond.L when calling
func (t *Topic[T]) shouldRun() bool {
	id := t.oldestPendingEventID()
	if id == nil {
		return false
	}
	return t.bus.lastCompletedEvent+1 == *id
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
