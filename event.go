package deterbus

import (
	"context"
	"reflect"
	"sync/atomic"
	"unsafe"
)

var handlerID = atomic.Uint64{}

type callbackType[T any] func(context.Context, T)

// eventHandler is a container around a function and metadata for
// that function.
type eventHandler struct {
	// id is used for unsubscription.
	id         uint64
	topic      *topicIdentifier
	callBack   interface{}
	argType    reflect.Type
	flagOnce   bool
	subscriber string
}

// argEvent is the args passed into eventHandler.
type argEvent[T any] struct {
	topic         *topicIdentifier
	ctxArg        context.Context
	arg           T
	eventNumber   uint64
	finished      chan interface{}
	transactional bool
}

func (ev *argEvent[T]) callWith(evh *eventHandler) {
	// unsafe cast the function interface pointer.
	// this is way faster than using reflect.Call
	faked := *(*callbackType[T])(unsafe.Pointer(&evh.callBack))
	faked(ev.ctxArg, ev.arg)
}

func newHandler(topic *topicIdentifier, once bool, trace string, fn interface{}, argType reflect.Type) eventHandler {
	// Wrap it up.
	return eventHandler{
		id:         handlerID.Add(1),
		topic:      topic,
		callBack:   fn,
		argType:    argType,
		flagOnce:   once,
		subscriber: trace,
	}
}
