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
	// events is a queue of runners
	events *queue.Queue
}

func New(ctx context.Context) *Bus {
	b := &Bus{
		eventCond: sync.NewCond(&sync.Mutex{}),
		events:    queue.New(),
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

func (b *Bus) publish(run runner) {
	b.eventCond.L.Lock()
	b.events.Add(run)
	b.eventCond.Signal()
	b.eventCond.L.Unlock()
}

// consume runs all the events on the queue
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

type callbackContainer[T any] struct {
	// id is unique per callback per topic.
	// this needs to exist to handle unsubscribe
	id uint64
	// fn is the actual callback function to invoke
	fn Callback[T]
}

// event is a topic event.
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

// subscribeEvent is an internal event that delays callback subscription
type subscribeEvent[T any] struct {
	topic    *Topic[T]
	callback *callbackContainer[T]
}

func (s *subscribeEvent[T]) run() {
	s.topic.callbackLock.Lock()
	defer s.topic.callbackLock.Unlock()
	s.topic.callbacks = append(s.topic.callbacks, s.callback)
}

// unsubscribeEvent is an internal event that delays callback removal
type unsubscribeEvent[T any] struct {
	topic      *Topic[T]
	callbackID uint64
}

func (u *unsubscribeEvent[T]) run() {
	u.topic.callbackLock.Lock()
	defer u.topic.callbackLock.Unlock()

	// Find the index for the handler.
	index := -1
	for i, handler := range u.topic.callbacks {
		if handler.id == u.callbackID {
			index = i
			break
		}
	}

	if index < 0 {
		// not found
		return
	}

	// it's the last one
	if len(u.topic.callbacks) == 1 {
		u.topic.callbacks = []*callbackContainer[T]{}
		return
	}

	// delete it
	u.topic.callbacks = append(u.topic.callbacks[:index], u.topic.callbacks[index+1:]...)
}
