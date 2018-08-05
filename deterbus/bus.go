package deterbus

import (
	"context"
	"fmt"
	"sync"
)

type metaEvent int

const (
	subscribeEvent metaEvent = iota
	unsubscribeEvent
)

// CtxKey is used to prevent collisions on additions
// to the context passed in to Subscribers.
type CtxKey int

const (
	// EventNumber uniquely identifies an event.
	EventNumber CtxKey = iota
	// EventTopic is the topic that invoked the handler.
	EventTopic
)

// Bus is a deterministic* event bus.
type Bus struct {
	currentNumber   uint64
	processedNumber uint64
	pendingEvents   []argEvent
	listeners       map[interface{}]([]*eventHandler)
	eventDone       *sync.Cond
	done            chan interface{}
	draining        bool
	locker          *sync.Mutex
}

// New creates a new Bus.
func New() Bus {
	eb := Bus{
		currentNumber:   0,
		processedNumber: 0,
		pendingEvents:   make([]argEvent, 0),
		listeners:       make(map[interface{}]([]*eventHandler)),
		eventDone:       sync.NewCond(&sync.Mutex{}),
		draining:        false,
		done:            make(chan interface{}),
	}

	// Subscribe and Unsubscribe are treated as events.
	// This helps make the whole thing more deterministic.
	// But to get this to work, we need to bootstrap them in.
	subHandler, _ := newHandler(subscribeEvent, false, subscribe)
	unsubHandler, _ := newHandler(unsubscribeEvent, false, unsubscribe)

	eb.listeners[subscribeEvent] = []*eventHandler{subHandler}
	eb.listeners[unsubscribeEvent] = []*eventHandler{unsubHandler}

	// Start consumer.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		wg.Done()
		for {
			select {
			case <-eb.done:
				return
			default:
				consume(eb)
			}
		}
	}()
	wg.Wait()
	return eb
}

// Stop shuts down all event processing.
// You may wish to Drain() first.
// Only call this when you are destroying the object,
// since processing can't be restarted.
func (eb *Bus) Stop() {
	close(eb.done)
}

// Drain prevents new events from being published,
// but will wait for already-queued events to finish
// being consumed. The channel returned indicates
// when draining is complete.
// Only call this when you are destroying the object,
// since events will be dropped while draining.
func (eb *Bus) Drain() <-chan interface{} {
	eb.locker.Lock()
	defer eb.locker.Unlock()

	eb.draining = true

	done := make(chan interface{})

	// Don't return until the goroutine starts.
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		wg.Done()
		// Loop until all events are gone.
		eb.eventDone.L.Lock()
		for eb.processedNumber != eb.currentNumber {
			eb.eventDone.Wait()
		}
		eb.eventDone.L.Unlock()

		close(done)
	}()

	wg.Wait()
	return done
}

// Publish adds an event to the queue.
// ctx is passed in as the first argument to the handler,
// followed by args.
// The returned channel indicates when the event
// has been completely consumed by subscribers (if any).
func (eb *Bus) Publish(ctx context.Context, topic interface{}, args ...interface{}) <-chan interface{} {
	eb.locker.Lock()
	defer eb.locker.Unlock()

	done := make(chan interface{})

	if eb.draining {
		close(done)
		return done
	}

	// Add event to the event queue, and
	// increment the event number.
	eb.eventDone.L.Lock()
	eNum := eb.currentNumber

	eb.pendingEvents = append(eb.pendingEvents,
		argEvent{
			topic: topic,
			ctx: context.WithValue(
				context.WithValue(ctx, EventTopic, topic), EventNumber, eNum),
			args:        args,
			eventNumber: eNum,
		})
	eb.currentNumber++
	eb.eventDone.L.Unlock()

	// Don't return until the goroutine starts.
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		wg.Done()
		// Loop until processedNumber matches the
		// the number of the event we stuck
		// into the queue.
		eb.eventDone.L.Lock()
		for eb.processedNumber != eNum {
			eb.eventDone.Wait()
		}
		eb.eventDone.L.Unlock()

		close(done)
	}()

	wg.Wait()
	return done
}

// consume runs in a go routine and pops off the oldest event
// in the queue and sends it to all listeners.
// This call blocks until all listeners are done.
// Once all listeners finish processing the event, the
// processedNumber will be increased.
func consume(eb Bus) {
	eb.locker.Lock()
	defer eb.locker.Unlock()

	// Pop off the event at the end of the queue
	endEvent := len(eb.pendingEvents) - 1
	ev := eb.pendingEvents[endEvent]
	eb.pendingEvents = eb.pendingEvents[:endEvent]

	// Increment the event number
	defer func() {
		eb.eventDone.L.Lock()
		defer eb.eventDone.L.Unlock()
		if eb.processedNumber != ev.eventNumber-1 {
			panic(fmt.Sprintf("events were processed out of order. expected %v, found %v", eb.processedNumber+1, ev.eventNumber))
		}
		eb.processedNumber = ev.eventNumber
	}()

	// Find the listeners for it
	listeners, ok := eb.listeners[ev.topic]

	// no listeners means we are done.
	if !ok {
		return
	}

	// figure out the params we need to send
	params := ev.createParams()

	// call all listeners concurrently and then wait for them to complete.
	var lwg sync.WaitGroup
	lwg.Add(len(listeners))

	// need to handle listeners subscribed with Once
	// before returning, so it's a special case for
	// unsubscribe.
	var removeLock sync.Mutex
	toRemove := make([]int, 0)

	for i, listener := range listeners {
		go func(i int, l *eventHandler) {
			defer lwg.Done()
			if l.flagOnce {
				removeLock.Lock()
				toRemove = append(toRemove, i)
				removeLock.Unlock()
			}
			l.call(params)
		}(i, listener)
	}

	// Wait until all listeners are done.
	lwg.Wait()

	// Remove listeners that only wanted a single event.
	if len(toRemove) > 0 {
		// go through list in reverse so no shifting
		// is needed when items are removed
		for i := len(listeners) - 1; i >= 0; i-- {
			for _, r := range toRemove {
				if i == r {
					listeners = append(listeners[:i], listeners[i+1:]...)
				}
			}
		}
	}

	// publish completion
	eb.eventDone.Broadcast()
}

// Subscribe adds a new handler for the given topic.
// Subcription is actually an event, so you don't need to
// worry about receiving events that haven't started processing
// yet.
// once indicates that the handler should unsubcribe after
// receiving its first call.
// fn should be a function whose first parameter is a context.Context.
// This function returns a channel indicating when the subscribe has
// taken effect, or an error if the params were incorrect.
func (eb *Bus) Subscribe(topic interface{}, once bool, fn interface{}) (<-chan interface{}, error) {
	evh, err := newHandler(topic, once, fn)
	if err != nil {
		done := make(chan interface{})
		defer close(done)
		return done, err
	}

	return eb.Publish(context.Background(), subscribeEvent, eb, evh), err
}

func subscribe(ctx context.Context, eb Bus, evh eventHandler) {
	eb.locker.Lock()
	defer eb.locker.Unlock()

	// Add the handler to the topic.
	_, ok := eb.listeners[evh.topic]
	// Topic doesn't exist yet, so create it.
	if !ok {
		eb.listeners[evh.topic] = make([]*eventHandler, 1)
	}
	// Add it to the list.
	eb.listeners[evh.topic] = append(eb.listeners[evh.topic], &evh)
}

// Unsubscribe removes a handler from the given topic.
// Unsubscription is actually an event, so you don't need to
// worry about missing events that haven't started processing
// yet.
// This function returns a channel indicating when the unsubscribe
// has taken effect.
func (eb *Bus) Unsubscribe(topic interface{}, fn interface{}) <-chan interface{} {
	return eb.Publish(context.Background(), unsubscribeEvent, eb, topic, fn)
}

func unsubscribe(ctx context.Context, eb Bus, topic interface{}, fn interface{}) {
	eb.locker.Lock()
	defer eb.locker.Unlock()

	v, ok := eb.listeners[topic]

	if !ok {
		return
	}

	// Find the index for the handler.
	index := -1
	for i, handler := range v {
		if handler.callBack == fn {
			index = i
			break
		}
	}

	if index == 0 {
		return
	}

	if len(v) == 1 {
		eb.listeners[topic] = make([]*eventHandler, 0)
		return
	}

	// Remove it.
	eb.listeners[topic] = append(v[:index], v[index+1:]...)
}
