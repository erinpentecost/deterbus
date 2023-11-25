package deterbus

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"sync"

	"github.com/eapache/queue"
)

type metaEvent int

const (
	subscribeEvent metaEvent = iota
	unsubscribeEvent
	errorEvent
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

var (
	ErrDraining = fmt.Errorf("bus is draining; publish rejected")
)

// Bus is a deterministic* event bus.
type Bus struct {
	options         busOptions
	publishedNumber uint64
	consumedNumber  uint64
	pendingEvents   *queue.Queue
	listeners       map[interface{}]([]*eventHandler)
	publishMethod   func(eb *Bus, topic interface{}, txn bool, args ...interface{}) (<-chan interface{}, error)
	done            chan interface{}
	queueLocker     *sync.Mutex
	eventWatcher    *sync.Cond
}

// New creates a new Bus.
func New(opts ...BusOption) *Bus {
	eb := Bus{
		options:         buildOptions(opts...),
		publishedNumber: 0,
		consumedNumber:  0,
		pendingEvents:   queue.New(),
		listeners:       make(map[interface{}]([]*eventHandler)),
		publishMethod:   publishNormal,
		done:            make(chan interface{}),
		queueLocker:     &sync.Mutex{},
		eventWatcher:    sync.NewCond(&sync.Mutex{}),
	}

	// Subscribe and Unsubscribe are treated as events.
	// This helps make the whole thing more deterministic.
	// But to get this to work, we need to bootstrap them in.
	subHandler := newHandler(subscribeEvent, false, "contructor", subscribe)
	unsubHandler := newHandler(unsubscribeEvent, false, "constructor", unsubscribe)

	eb.listeners[subscribeEvent] = []*eventHandler{&subHandler}
	eb.listeners[unsubscribeEvent] = []*eventHandler{&unsubHandler}

	// Start consumer.
	go func() {
		for {
			select {
			case <-eb.done:
				return
			default:
				// TODO: Suspend the go routine instead of
				// spinwaiting.

				<-processEvent(&eb)
			}
		}
	}()

	return &eb
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

	// Stop accepting new events
	eb.queueLocker.Lock()
	eb.publishMethod = publishDraining
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

		// Stop all processing.
		eb.Stop()
	}()

	return done
}

// Wait returns a chan that will close when there
// are no pending events that need to be processed.
func (eb *Bus) Wait() <-chan interface{} {
	done := make(chan interface{})

	go func() {
		defer close(done)
		// Loop until all events are gone.
		eb.eventWatcher.L.Lock()
		for eb.consumedNumber < eb.publishedNumber {
			eb.eventWatcher.Wait()
		}
		eb.eventWatcher.L.Unlock()
	}()

	return done
}

// publish adds an event to the queue. Handlers will be called concurrently.
// The returned channel indicates when the event
// has been completely consumed by subscribers (if any).
func (eb *Bus) publish(topic interface{}, args ...interface{}) (<-chan interface{}, error) {
	return eb.publishMethod(eb, topic, false, args...)
}

// publishSerially adds an event to the queue.
// Listeners will process the event serially and deterministically.
// The returned channel indicates when the event
// has been completely consumed by subscribers (if any).
func (eb *Bus) publishSerially(topic interface{}, args ...interface{}) (<-chan interface{}, error) {
	return eb.publishMethod(eb, topic, true, args...)
}

func publishDraining(eb *Bus, topic interface{}, txn bool, args ...interface{}) (<-chan interface{}, error) {
	done := make(chan interface{})
	close(done)
	return done, ErrDraining
}

// Store reflected type of context
var ctxType reflect.Type

func init() {
	ctxType = reflect.TypeOf((*context.Context)(nil)).Elem()
}

func publishNormal(eb *Bus, topic interface{}, txn bool, args ...interface{}) (<-chan interface{}, error) {
	done := make(chan interface{})

	// I need to claim the publish number before adding to queue
	eb.eventWatcher.L.Lock()
	pubNum := eb.publishedNumber + 1
	eb.publishedNumber = pubNum
	eb.eventWatcher.L.Unlock()

	//  Add on context values if first arg is a context.
	if (len(args) > 0) && (reflect.TypeOf(args[0]).Implements(ctxType)) {
		args[0] = context.WithValue(
			context.WithValue(
				reflect.ValueOf(args[0]).Interface().(context.Context), EventTopic, topic), EventNumber, pubNum)
	}

	ev := argEvent{
		topic:         topic,
		args:          args,
		eventNumber:   pubNum,
		finished:      done,
		transactional: txn,
	}

	// This results in unmanaged locking of the entire
	// eventbus and starves the consumer.
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()
	eb.pendingEvents.Add(ev)

	// Broadcast the publish count change AFTER adding the event to the queue.
	// I want to make sure data is available when I send the signal.
	eb.eventWatcher.L.Lock()
	eb.eventWatcher.Broadcast()
	eb.eventWatcher.L.Unlock()

	return done, nil
}

// processEvent obtains a lock.
func processEvent(eb *Bus) <-chan interface{} {
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()
	done := make(chan interface{})
	defer close(done)

	// Quit if there are no events.
	if eb.pendingEvents.Length() == 0 {
		// Yield thread, since this spins forever.
		runtime.Gosched()

		return done
	}

	// Pop off the first event.
	ev := eb.pendingEvents.Remove().(argEvent)
	defer close(ev.finished)

	// Find the listeners for it
	listeners, ok := eb.listeners[ev.topic]

	// If there are no listeners, just quit.
	if !ok || len(listeners) == 0 {
		// Publish the signal AFTER the event is handled
		// and removed from the queue, while a lock still exists.
		eb.eventWatcher.L.Lock()
		eb.consumedNumber = ev.eventNumber
		eb.eventWatcher.Broadcast()
		eb.eventWatcher.L.Unlock()
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
		if listener == nil {
			panic(fmt.Sprintf("%vth listener for %s is nil", i, ev.topic))
		}

		// Actually invoke the listener.
		invokeHndlr := func(l *eventHandler) {
			defer lwg.Done()

			// Catch panics, but not if this panic is from
			// a panic handler itself.
			defer func() {
				if r := recover(); (r != nil) && (ev.topic != errorEvent) {
					eb.publish(errorEvent, SubscriberPanic{
						internal:      r,
						topic:         ev.topic,
						publishNumber: ev.eventNumber,
						subscriber:    l.subscriber,
					})
				}
			}()

			l.call(params)
		}

		if ev.transactional {
			invokeHndlr(listener)
		} else {
			go invokeHndlr(listener)
		}

		// Remove it from the list if needed.
		if listener.flagOnce {
			listeners = append(listeners[:i], listeners[i+1:]...)
		}
	}

	// Unlock queue early so publishers can add to it
	// while we suspend for the listeners.
	eb.queueLocker.Unlock()

	// Wait until all listeners are done.
	lwg.Wait()

	// Publish the signal AFTER the event is handled
	// and removed from the queue, while a lock still exists.
	eb.eventWatcher.L.Lock()
	eb.consumedNumber = ev.eventNumber
	eb.eventWatcher.Broadcast()
	eb.eventWatcher.L.Unlock()

	// Mark that the event is done.
	eb.queueLocker.Lock()

	return done
}

// Unsubscribe will unsubscribe a handler from a topic.
type Unsubscribe func() <-chan interface{}

// SubscribeToPanic lets you handle panics called by your callback
// functions. This can be useful for logging.
func (eb *Bus) SubscribeToPanic(fn func(SubscriberPanic)) (<-chan interface{}, Unsubscribe) {
	return eb.subscribeImplementation(errorEvent, false, fn)
}

// subscribe adds a new handler for the given topic.
// Subcription is actually an event, so you don't need to
// worry about receiving events that haven't started processing
// yet.
// This function returns a channel indicating when the subscribe has
// taken effect, or an error if the params were incorrect.
func (eb *Bus) subscribe(topic interface{}, fn interface{}) (<-chan interface{}, Unsubscribe) {
	return eb.subscribeImplementation(topic, false, fn)
}

// subscribeOnce adds a new handler for the given topic.
// Subcription is actually an event, so you don't need to
// worry about receiving events that haven't started processing
// yet.
// Once indicates that the handler should unsubscribe after
// receiving its first call.
// This function returns a channel indicating when the subscribe has
// taken effect, or an error if the params were incorrect.
func (eb *Bus) subscribeOnce(topic interface{}, fn interface{}) (<-chan interface{}, Unsubscribe) {
	return eb.subscribeImplementation(topic, true, fn)
}

func (eb *Bus) subscribeImplementation(topic interface{}, once bool, fn interface{}) (<-chan interface{}, Unsubscribe) {
	// Store information on caller.
	_, callerFile, callerLine, ok := runtime.Caller(2)
	callerMsg := "unknown"
	if ok {
		callerMsg = fmt.Sprintf("%s [%v]", callerFile, callerLine)
	}

	// check shapeMap
	fnType := reflect.TypeOf(fn)
	argTypes := make([]reflect.Type, fnType.NumIn())
	for i := range argTypes {
		argTypes[i] = fnType.In(i)
	}

	evh := newHandler(topic, once, callerMsg, fn)

	// Create the listener collection
	eb.queueLocker.Lock()
	currentListeners, ok := eb.listeners[evh.topic]
	// Topic doesn't exist yet, so create it.
	if (!ok) || currentListeners == nil {
		eb.listeners[evh.topic] = make([]*eventHandler, 0)
	}
	eb.queueLocker.Unlock()

	id := evh.id
	unsub := func() <-chan interface{} {
		return eb.unsubscribe(topic, id)
	}

	sub, _ := eb.publish(subscribeEvent, eb, evh)
	// Publish only returns an error when draining.
	// Drop the error during subscribe.
	return sub, unsub
}

// This is the subscribe handler.
func subscribe(eb *Bus, evh eventHandler) {
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()

	// Add it to the list.
	eb.listeners[evh.topic] = append(eb.listeners[evh.topic], &evh)
}

// unsubscribe removes a handler from the given topic.
// Unsubscription is actually an event, so you don't need to
// worry about missing events that haven't started processing
// yet.
// This function returns a channel indicating when the unsubscribe
// has taken effect.
func (eb *Bus) unsubscribe(topic interface{}, id uint64) <-chan interface{} {
	done, _ := eb.publish(unsubscribeEvent, eb, topic, id)
	// Publish only returns an error when draining.
	// Drop the error during unsubscribe.
	return done
}

// This is the unsubscribe handler.
func unsubscribe(eb *Bus, topic interface{}, id uint64) {
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()

	v, ok := eb.listeners[topic]

	if !ok {
		return
	}

	// Find the index for the handler.
	index := -1
	for i, handler := range v {
		if handler.id == id {
			index = i
			break
		}
	}

	if index < 0 {
		return
	}

	if len(v) == 1 {
		eb.listeners[topic] = make([]*eventHandler, 0)
		return
	}

	// Remove it.
	eb.listeners[topic] = append(v[:index], v[index+1:]...)
}
