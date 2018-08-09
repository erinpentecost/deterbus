package deterbus

import (
	"context"
	"errors"
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
	pendingEvents   *queue.Queue
	listeners       map[interface{}]([]*eventHandler)
	publishMethod   func(eb *Bus, topic interface{}, args ...interface{}) (<-chan interface{}, error)
	done            chan interface{}
	queueLocker     *sync.Mutex
	eventWatcher    *sync.Cond
}

// New creates a new Bus.
func New() *Bus {
	eb := Bus{
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
	subHandler, subErr := newHandler(subscribeEvent, false, subscribe)
	unsubHandler, unsubErr := newHandler(unsubscribeEvent, false, unsubscribe)

	if subErr != nil {
		panic(subErr)
	}
	if unsubErr != nil {
		panic(unsubErr)
	}

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
				// Yield thread, since this spins forever.
				runtime.Gosched()
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
		close(eb.done)
	}()

	return done
}

// Publish adds an event to the queue.
// ctx is passed in as the first argument to the handler,
// followed by args.
// The returned channel indicates when the event
// has been completely consumed by subscribers (if any).
func (eb *Bus) Publish(topic interface{}, args ...interface{}) (<-chan interface{}, error) {
	return eb.publishMethod(eb, topic, args...)
}

func publishDraining(eb *Bus, topic interface{}, args ...interface{}) (<-chan interface{}, error) {
	done := make(chan interface{})
	close(done)
	return done, errors.New("bus is draining")
}

// Store reflected type of context
var ctxType reflect.Type

func init() {
	ctxType = reflect.TypeOf((*context.Context)(nil)).Elem()
}

func publishNormal(eb *Bus, topic interface{}, args ...interface{}) (<-chan interface{}, error) {
	done := make(chan interface{})

	// I need to claim the publish number before adding to queue
	eb.eventWatcher.L.Lock()
	pubNum := eb.publishedNumber + 1
	eb.publishedNumber = pubNum
	eb.eventWatcher.L.Unlock()

	//  Add on context values if first arg is a context.
	if (len(args) > 0) && (reflect.TypeOf(args[0]) == ctxType) {
		args[0] = context.WithValue(
			context.WithValue(
				reflect.ValueOf(args[0]).Interface().(context.Context), EventTopic, topic), EventNumber, pubNum)
	}

	ev := argEvent{
		topic:       topic,
		args:        args,
		eventNumber: pubNum,
		finished:    done,
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

	if eb.pendingEvents.Length() == 0 {
		return done
	}

	ev := eb.pendingEvents.Remove().(argEvent)
	defer close(ev.finished)

	// Find the listeners for it
	listeners, ok := eb.listeners[ev.topic]

	// If there are no listeners, just quit.
	if !ok || len(listeners) == 0 {
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
	// while we suspend for the listeners.
	eb.queueLocker.Unlock()

	// Wait until all listeners are done.
	lwg.Wait()

	// Mark that the event is done.
	eb.queueLocker.Lock()

	// Publish the signal AFTER the event is handled
	// and removed from the queue, while a lock still exists.
	eb.eventWatcher.L.Lock()
	eb.consumedNumber = ev.eventNumber
	eb.eventWatcher.Broadcast()
	eb.eventWatcher.L.Unlock()

	return done
}

// Subscribe adds a new handler for the given topic.
// Subcription is actually an event, so you don't need to
// worry about receiving events that haven't started processing
// yet.
// This function returns a channel indicating when the subscribe has
// taken effect, or an error if the params were incorrect.
func (eb *Bus) Subscribe(topic interface{}, fn interface{}) (<-chan interface{}, error) {
	return eb.subscribeImplementation(topic, false, fn)
}

// SubscribeOnce adds a new handler for the given topic.
// Subcription is actually an event, so you don't need to
// worry about receiving events that haven't started processing
// yet.
// Once indicates that the handler should unsubcribe after
// receiving its first call.
// This function returns a channel indicating when the subscribe has
// taken effect, or an error if the params were incorrect.
func (eb *Bus) SubscribeOnce(topic interface{}, fn interface{}) (<-chan interface{}, error) {
	return eb.subscribeImplementation(topic, true, fn)
}

func (eb *Bus) subscribeImplementation(topic interface{}, once bool, fn interface{}) (<-chan interface{}, error) {
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
	if (!ok) || currentListeners == nil {
		eb.listeners[evh.topic] = make([]*eventHandler, 0)
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

	return eb.Publish(subscribeEvent, eb, evh)
}

// This is the subscribe handler.
func subscribe(eb *Bus, evh eventHandler) {
	eb.queueLocker.Lock()
	defer eb.queueLocker.Unlock()

	// Add it to the list.
	eb.listeners[evh.topic] = append(eb.listeners[evh.topic], &evh)
}

// Unsubscribe removes a handler from the given topic.
// Unsubscription is actually an event, so you don't need to
// worry about missing events that haven't started processing
// yet.
// This function returns a channel indicating when the unsubscribe
// has taken effect.
func (eb *Bus) Unsubscribe(topic interface{}, fn interface{}) (<-chan interface{}, error) {
	return eb.Publish(unsubscribeEvent, eb, topic, fn)
}

// This is the unsubscribe handler.
func unsubscribe(eb *Bus, topic interface{}, fn interface{}) {
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
