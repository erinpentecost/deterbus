package deterbus

import (
	"context"
	"runtime"
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
	publishedNumber uint64
	consumedNumber  uint64
	pendingEvents   []argEvent
	listeners       map[interface{}]([]*eventHandler)
	done            chan interface{}
	draining        bool
	queueLocker     *sync.Mutex
	heartBeat       chan EventPulse
}

// New creates a new Bus.
func New() Bus {
	eb := Bus{
		publishedNumber: 0,
		consumedNumber:  0,
		pendingEvents:   make([]argEvent, 0),
		listeners:       make(map[interface{}]([]*eventHandler)),
		draining:        false,
		done:            make(chan interface{}),
		queueLocker:     &sync.Mutex{},
		heartBeat:       make(chan EventPulse, 1),
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
				// TODO: This is just a busy spinwait loop.
				// Refactor so consume() is not being invoked
				// all the time.
				consume(eb)
				runtime.Gosched()
			}
		}
	}()
	wg.Wait()
	return eb
}

func (eb *Bus) pop() *argEvent {
	if len(eb.pendingEvents) == 0 {
		return nil
	}

	endEventIndex := len(eb.pendingEvents) - 1
	ev := eb.pendingEvents[endEventIndex]
	eb.pendingEvents = eb.pendingEvents[:endEventIndex]
	return &ev
}

// EventPulse is an indicator type sent out on the heartbeat channel.
type EventPulse struct {
	PublishedEvents uint64
	ConsumedEvents  uint64
}

// GetMonitor returns a heartbeat monitor that can be
// inspected to identify when events are being processed.
func (eb *Bus) GetMonitor() <-chan EventPulse {
	return eb.heartBeat
}
func (eb *Bus) pulseMonitor() {
	select {
	case eb.heartBeat <- EventPulse{PublishedEvents: eb.publishedNumber, ConsumedEvents: eb.consumedNumber}:
	default:
	}
}

// Stop shuts down all event processing.
// You may wish to Drain() first.
// Only call this when you are destroying the object,
// since processing can't be restarted.
func (eb *Bus) Stop() {
	close(eb.done)
	close(eb.heartBeat)
}

// Drain prevents new events from being published,
// but will wait for already-queued events to finish
// being consumed. The channel returned indicates
// when draining is complete.
// Only call this when you are destroying the object,
// since events will be dropped while draining.
func (eb *Bus) Drain() <-chan interface{} {
	eb.queueLocker.Lock()
	eb.draining = true
	eb.queueLocker.Unlock()

	done := make(chan interface{})

	// Don't return until the goroutine starts.
	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		wg.Done()
		defer close(done)
		// Loop until all events are gone.
		panic("not implemented yet")

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
	done := make(chan interface{})
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()

	// Quit early if we are draining.
	if eb.draining {
		close(done)
		return done
	}

	// Add event to the event queue, and
	// increment the event number.

	// Create the event we need to send.
	pubNum := eb.publishedNumber + 1
	eb.publishedNumber = pubNum

	ev := argEvent{
		topic: topic,
		ctx: context.WithValue(
			context.WithValue(ctx, EventTopic, topic), EventNumber, pubNum),
		args:        args,
		eventNumber: pubNum,
		finished:    done,
	}

	// Indicate that work is being done.
	//eb.pulseMonitor()

	eb.pendingEvents = append(eb.pendingEvents, ev)

	return done
}

// consume runs in a go routine and pops off the oldest event
// in the queue and sends it to all listeners.
// This call blocks until all listeners are done.
// Once all listeners finish processing the event, the
// processedNumber will be increased.
func consume(eb Bus) {
	eb.queueLocker.Lock()

	// Pop off the event at the end of the queue
	ev := eb.pop()

	if ev == nil {
		eb.queueLocker.Unlock()
		return
	}

	// Find the listeners for it
	listeners, ok := eb.listeners[ev.topic]

	// no listeners means we are done.
	if ok && len(listeners) > 0 {
		// figure out the params we need to send
		params := ev.createParams()

		// call all listeners concurrently and then wait for them to complete.
		var lwg sync.WaitGroup
		lwg.Add(len(listeners))

		// need to handle listeners subscribed with Once
		// before returning, so it's a special case for
		// unsubscribe.
		for i := len(listeners) - 1; i >= 0; i-- {
			listener := listeners[i]
			// Actually invoke the listener.
			go func(l *eventHandler) {
				defer lwg.Done()
				l.call(params)
			}(listener)
			// Remove it from the list if needed.
			if listener.flagOnce {
				listeners = append(listeners[:i], listeners[i+1:]...)
			}
		}

		// Unlock queue early so publishers can add to it
		// while we spin on the listeners.
		eb.queueLocker.Unlock()

		// Wait until all listeners are done.
		lwg.Wait()

		eb.queueLocker.Lock()
		// Mark that the event is done.
		eb.consumedNumber = ev.eventNumber
		close(ev.finished)

		eb.queueLocker.Unlock()
	} else {
		// Mark that the event is done.
		eb.consumedNumber = ev.eventNumber

		close(ev.finished)

		eb.queueLocker.Unlock()
	}
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

	if index <= 0 {
		return
	}

	if len(v) == 1 {
		eb.listeners[topic] = make([]*eventHandler, 0)
		return
	}

	// Remove it.
	eb.listeners[topic] = append(v[:index], v[index+1:]...)
}
