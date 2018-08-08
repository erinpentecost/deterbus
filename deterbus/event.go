package deterbus

import (
	"context"
	"fmt"
	"reflect"
)

// eventHandler is a container around a function and metadata for
// that function.
type eventHandler struct {
	topic    interface{}
	callBack reflect.Value
	flagOnce bool
	shape    reflect.Type
}

// argEvent is the args passed into eventHandler.
type argEvent struct {
	topic       interface{}
	ctx         context.Context
	args        []interface{}
	eventNumber uint64
	finished    chan interface{}
}

func (ev *argEvent) createParams() []reflect.Value {
	// Create arguments to pass in via reflection.
	// Context needs to be prepended.
	params := []reflect.Value{reflect.ValueOf(ev.ctx)}
	for _, arg := range ev.args {
		params = append(params, reflect.ValueOf(arg))
	}

	return params
}

func (evh *eventHandler) call(params []reflect.Value) {
	// Actually call the function.
	evh.callBack.Call(params)
}

// Store reflected type of context
var ctxType reflect.Type

func init() {
	ctxType = reflect.TypeOf((*context.Context)(nil)).Elem()
}

func newHandler(topic interface{}, once bool, fn interface{}) (eventHandler, error) {

	// Verify input.
	if reflect.TypeOf(fn).Kind() != reflect.Func {
		return eventHandler{}, fmt.Errorf("topic %s: %s is not of type reflect.Func", topic, reflect.TypeOf(fn).Kind())
	}
	if reflect.Type.NumIn(reflect.TypeOf(fn)) == 0 {
		return eventHandler{}, fmt.Errorf("topic %s: function must have at least one parameter", topic)
	}
	firstParam := reflect.Type.In(reflect.TypeOf(fn), 0)
	if !firstParam.Implements(ctxType) {
		return eventHandler{}, fmt.Errorf("topic %s: function's first parameter must implement %s, not %s",
			topic,
			ctxType.Name(),
			firstParam.Name())
	}

	// Wrap it up.
	return eventHandler{
		topic:    topic,
		callBack: reflect.ValueOf(fn),
		flagOnce: once,
		shape:    reflect.TypeOf(fn),
	}, nil
}
