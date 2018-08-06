package deterbus

import (
	"context"
	"errors"
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
	for _, arg := range params {
		params = append(params, reflect.ValueOf(arg))
	}

	return params
}

func (evh *eventHandler) call(params []reflect.Value) {
	hParamCount := evh.shape.NumIn()
	if hParamCount != len(params) {
		panic(fmt.Sprintf("expected %v params, not %v", hParamCount, len(params)))
	}

	// Actually call the function.
	evh.callBack.Call(params)
}

// Store reflected type of context
var ctxType reflect.Type

func init() {
	ctxType = reflect.TypeOf((*context.Context)(nil)).Elem()
}

func newHandler(topic interface{}, once bool, fn interface{}) (*eventHandler, error) {

	// Verify input.
	if reflect.TypeOf(fn).Kind() != reflect.Func {
		return nil, fmt.Errorf("%s is not of type reflect.Func", reflect.TypeOf(fn).Kind())
	}
	if reflect.Type.NumIn(reflect.TypeOf(fn)) == 0 {
		return nil, errors.New("function must have at least one parameter")
	}
	firstParam := reflect.Type.In(reflect.TypeOf(fn), 0)
	if !firstParam.Implements(ctxType) {
		return nil, fmt.Errorf("function's first parameter must implement %s, not %s",
			ctxType.Name(),
			firstParam.Name())
	}

	// Wrap it up.
	return &eventHandler{
		topic:    topic,
		callBack: reflect.ValueOf(fn),
		flagOnce: once,
		shape:    reflect.TypeOf(fn),
	}, nil
}
