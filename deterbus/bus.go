package deterbus

import (
	"context"
	"errors"
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
	publishedNumber uint64
	consumedNumber  uint64
	pendingEvents   []argEvent
	listeners       map[interface{}]([]*eventHandler)
	done            chan interface{}
	draining        bool
	queueLocker     *sync.Mutex
	eventWatcher    *sync.Cond
}

// New creates a new Bus.
func New() (*Bus, error) {
	eb := Bus{
		publishedNumber: 0,
		consumedNumber:  0,
		pendingEvents:   make([]argEvent, 0),
		listeners:       make(map[interface{}]([]*eventHandler)),
		draining:        false,
		done:            make(chan interface{}),
		queueLocker:     &sync.Mutex{},
		eventWatcher:    sync.NewCond(&sync.Mutex{}),
	}

	// Subscribe and Unsubscribe are treated as events.
	// This helps make the whole thing more deterministic.
	// But to get this to work, we need to bootstrap them in.
	subHandler, subErr := newHandler(subscribeEvent, false, subscribe)
	unsubHandler, unsubErr := newHandler(unsubscribeEvent, false, unsubscribe)

	if subErr != nil {
		return nil, subErr
	}
	if unsubErr != nil {
		return nil, unsubErr
	}

	eb.listeners[subscribeEvent] = []*eventHandler{subHandler}
	eb.listeners[unsubscribeEvent] = []*eventHandler{unsubHandler}

	// Start consumer.
	go func() {
		for {
			select {
			case <-eb.done:
				return
			default:
				// TODO: Suspend the go routine instead of
				// spinwaiting.
				// Loop while events are available.
				//eb.eventWatcher.L.Lock()
				//for eb.consumedNumber > eb.publishedNumber {
				//	eb.eventWatcher.Wait()
				//}
				//eb.eventWatcher.L.Unlock()

				ev := eb.pop()
				<-processEvent(&eb, ev)
			}
		}
	}()

	return &eb, nil
}

// pop obtains a lock
func (eb *Bus) pop() *argEvent {
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()

	if len(eb.pendingEvents) == 0 {
		return nil
	}

	endEventIndex := len(eb.pendingEvents) - 1
	ev := eb.pendingEvents[endEventIndex]
	eb.pendingEvents = eb.pendingEvents[:endEventIndex]
	return &ev
}

// Stop shuts down all event processing,
// throwing away queued events.
// Only call this when you are destroying the object,
// since processing can't be restarted.
func (eb *Bus) Stop() {
	close(eb.done)
}

// DrainStop prevents new events from being published,
// but will wait for already-queued events to finish
// being consumed. The channel returned indicates
// when draining is complete.
// Once complete, the Bus will be closed down.
// Only call this when you are destroying the object,
// since events will be dropped while draining.
func (eb *Bus) DrainStop() <-chan interface{} {
	eb.queueLocker.Lock()
	eb.draining = true
	eb.queueLocker.Unlock()

	done := make(chan interface{})

	go func() {
		defer close(done)
		// Loop until all events are gone.
		eb.eventWatcher.L.Lock()
		for eb.consumedNumber < eb.publishedNumber {
			eb.eventWatcher.Wait()
		}
		eb.eventWatcher.L.Unlock()
		close(eb.done)
	}()

	return done
}

// Publish adds an event to the queue.
// ctx is passed in as the first argument to the handler,
// followed by args.
// The returned channel indicates when the event
// has been completely consumed by subscribers (if any).
func (eb *Bus) Publish(ctx context.Context, topic interface{}, args ...interface{}) (<-chan interface{}, error) {
	done := make(chan interface{})
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()

	// Quit early if we are draining.
	if eb.draining {
		close(done)
		return done, errors.New("bus is draining")
	}

	// Check that the arg count is correct
	currentListeners, ok := eb.listeners[topic]
	if ok && len(currentListeners) > 0 {
		listenerInput, err := getInputTypes(currentListeners[0].shape)
		if err != nil {
			close(done)
			return done, err
		}
		argSlice := make([]interface{}, len(args)+1)
		argSlice = append(argSlice, ctx)
		argSlice = append(argSlice, args...)
		ok, err := typesMatch(getTypes(argSlice), listenerInput)
		if !ok {
			close(done)
			return done, err
		}
	}

	// Create the event we need to send.
	eb.eventWatcher.L.Lock()
	pubNum := eb.publishedNumber + 1
	eb.publishedNumber = pubNum
	eb.eventWatcher.Broadcast()
	eb.eventWatcher.L.Unlock()

	ev := argEvent{
		topic: topic,
		ctx: context.WithValue(
			context.WithValue(ctx, EventTopic, topic), EventNumber, pubNum),
		args:        args,
		eventNumber: pubNum,
		finished:    done,
	}

	eb.pendingEvents = append(eb.pendingEvents, ev)

	return done, nil
}

// processEvent obtains a lock.
func processEvent(eb *Bus, ev *argEvent) <-chan interface{} {
	done := make(chan interface{})
	defer close(done)

	if ev == nil {
		return done
	}

	eb.queueLocker.Lock()

	// Find the listeners for it
	listeners, ok := eb.listeners[ev.topic]

	// If there are no listeners, just quit.
	if !ok || len(listeners) == 0 {
		eb.queueLocker.Unlock()
		return done
	}

	// call all listeners concurrently and then wait for them to complete.
	var lwg sync.WaitGroup
	lwg.Add(len(listeners))

	params := ev.createParams()

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

	// Mark that the event is done.
	eb.queueLocker.Lock()
	eb.eventWatcher.L.Lock()
	eb.consumedNumber = ev.eventNumber
	eb.eventWatcher.Broadcast()
	eb.eventWatcher.L.Unlock()
	eb.queueLocker.Unlock()
	close(ev.finished)

	return done
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

	// Create the listener collection
	eb.queueLocker.Lock()
	currentListeners, ok := eb.listeners[evh.topic]
	// Topic doesn't exist yet, so create it.
	if !ok {
		eb.listeners[evh.topic] = make([]*eventHandler, 1)
	} else if len(currentListeners) > 0 {
		// make sure the function type is the same as other listeners
		if currentListeners[0].shape != evh.shape {
			done := make(chan interface{})
			defer close(done)
			eb.queueLocker.Unlock()
			return done, fmt.Errorf("new subscriber for %s has a different function definition", evh.topic)
		}

	}
	eb.queueLocker.Unlock()

	return eb.Publish(context.Background(), subscribeEvent, eb, evh)
}

func subscribe(ctx context.Context, eb *Bus, evh *eventHandler) {
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()

	// Add it to the list.
	eb.listeners[evh.topic] = append(eb.listeners[evh.topic], evh)
}

// Unsubscribe removes a handler from the given topic.
// Unsubscription is actually an event, so you don't need to
// worry about missing events that haven't started processing
// yet.
// This function returns a channel indicating when the unsubscribe
// has taken effect.
func (eb *Bus) Unsubscribe(topic interface{}, fn interface{}) (<-chan interface{}, error) {
	return eb.Publish(context.Background(), unsubscribeEvent, eb, topic, fn)
}

func unsubscribe(ctx context.Context, eb *Bus, topic interface{}, fn interface{}) {
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()

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
