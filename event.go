package deterbus

import (
	"reflect"
	"sync/atomic"
)

var handlerID = atomic.Uint64{}

// eventHandler is a container around a function and metadata for
// that function.
type eventHandler struct {
	// id is used for unsubscription.
	id         uint64
	topic      *topicIdentifier
	callBack   reflect.Value
	flagOnce   bool
	subscriber string
}

// argEvent is the args passed into eventHandler.
type argEvent struct {
	topic         *topicIdentifier
	args          []interface{}
	eventNumber   uint64
	finished      chan interface{}
	transactional bool
}

func (ev *argEvent) createParams() []reflect.Value {
	// Create arguments to pass in via reflection.
	params := make([]reflect.Value, len(ev.args))
	for i, arg := range ev.args {
		params[i] = reflect.ValueOf(arg)
	}

	return params
}

func (evh *eventHandler) call(params []reflect.Value) {
	// Actually call the function.
	evh.callBack.Call(params)
}

func newHandler(topic *topicIdentifier, once bool, trace string, fn interface{}) eventHandler {
	// Wrap it up.
	return eventHandler{
		id:         handlerID.Add(1),
		topic:      topic,
		callBack:   reflect.ValueOf(fn),
		flagOnce:   once,
		subscriber: trace,
	}
}
