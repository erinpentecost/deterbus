package deterbus

import (
	"fmt"
	"reflect"
)

// eventHandler is a container around a function and metadata for
// that function.
type eventHandler struct {
	topic      interface{}
	callBack   reflect.Value
	flagOnce   bool
	subscriber string
}

// argEvent is the args passed into eventHandler.
type argEvent struct {
	topic         interface{}
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

func newHandler(topic interface{}, once bool, trace string, fn interface{}) (eventHandler, error) {

	// Verify input.
	if reflect.TypeOf(fn).Kind() != reflect.Func {
		return eventHandler{}, fmt.Errorf("topic %s: %s is not of type reflect.Func", topic, reflect.TypeOf(fn).Kind())
	}

	// Wrap it up.
	return eventHandler{
		topic:      topic,
		callBack:   reflect.ValueOf(fn),
		flagOnce:   once,
		subscriber: trace,
	}, nil
}
