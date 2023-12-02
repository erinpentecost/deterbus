package bus

import (
	"context"
	"sync"

	"github.com/eapache/queue"
)

type runner interface {
	run()
}

// Bus just controls when topic handlers are invoked.
type Bus struct {
	eventCond *sync.Cond
	events    *queue.Queue

	cancelled bool
	cancel    context.CancelFunc
}

func New(ctx context.Context) *Bus {
	ctx, cancel := context.WithCancel(ctx)
	b := &Bus{
		eventCond: sync.NewCond(&sync.Mutex{}),
		events:    queue.New(),
		cancel:    cancel,
	}

	// use wg so we don't return the bus until
	// background goroutines have started.
	wg := &sync.WaitGroup{}
	wg.Add(2)
	// trigger broadcast when context expires
	go func(ctx context.Context, c *sync.Cond) {
		wg.Done()
		<-ctx.Done()
		c.Broadcast()
	}(ctx, b.eventCond)

	go b.consume(ctx, wg)

	wg.Wait()

	return b
}

func (b *Bus) Stop() {
	b.cancelled = true
	b.cancel()
}

func (b *Bus) consume(ctx context.Context, wg *sync.WaitGroup) {
	wg.Done()
	for {
		// wait until there is work
		b.eventCond.L.Lock()
		for b.events.Length() == 0 && ctx.Err() == nil {
			b.eventCond.Wait()
		}
		// check if done
		if ctx.Err() != nil {
			return
		}
		// pop off work
		run := b.events.Remove().(runner)
		// release lock
		b.eventCond.L.Unlock()

		// do work.
		// inside this method, a topic's callbackLock is held.
		run.run()
	}
}

func NewTopic[T any](bus *Bus) *Topic[T] {
	t := &Topic[T]{
		bus:          bus,
		callbackLock: &sync.Mutex{},
		callbacks:    []*callbackContainer[T]{},
	}
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
	topic  *Topic[T]
	wg     *sync.WaitGroup
}

func (e *event[T]) run() {
	// signal done for wait()
	defer e.wg.Done()

	// hold callbackLock so we can range on callbacks
	e.topic.callbackLock.Lock()
	defer e.topic.callbackLock.Unlock()

	// invoke callbacks
	for _, cc := range e.topic.callbacks {
		cc.fn(e.ctxArg, e.arg)
	}
}

// Topic is what users interact with.
// Each one watches the station to know when it must run.
type Topic[T any] struct {
	bus *Bus

	// newer callbacks must go on the end
	callbackLock   *sync.Mutex
	callbacks      []*callbackContainer[T]
	nextCallbackID uint64
}

// Waiter can be used to signal when all Subscribers have finished
// processing an event.
type Waiter interface {
	Wait()
}

func (t *Topic[T]) Publish(ctx context.Context, arg T) Waiter {
	wg := sync.WaitGroup{}
	wg.Add(1)
	t.bus.eventCond.L.Lock()
	ev := &event[T]{
		ctxArg: ctx,
		arg:    arg,
		topic:  t,
		wg:     &wg,
	}
	t.bus.events.Add(ev)
	t.bus.eventCond.Signal()
	t.bus.eventCond.L.Unlock()
	return &wg
}

func (t *Topic[T]) Subscribe(callback Callback[T]) Unsubscribe {
	// we hold callbackLock during event running
	t.callbackLock.Lock()
	defer t.callbackLock.Unlock()
	// increment id
	id := t.nextCallbackID
	t.nextCallbackID += 1

	cc := &callbackContainer[T]{
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
			t.callbacks = []*callbackContainer[T]{}
			return
		}

		// delete it
		t.callbacks = append(t.callbacks[:index], t.callbacks[index+1:]...)
	}
}
