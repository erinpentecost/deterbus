package deterbus

import (
	"context"
	"reflect"
	"sync/atomic"
)

// Dynamic topic ID generator that won't conflict with anything else.
type genericTopicIDType uint64

var genericTopicID atomic.Uint64

// TopicDefinition is a definition for a topic.
// It exists to hold the type T.
type TopicDefinition[T any] struct{}

// RegisterOn registers a TopicDefinition onto a Bus to get a Topic.
func (d *TopicDefinition[T]) RegisterOn(bus *Bus) *Topic[T] {
	return &Topic[T]{
		Bus:             bus,
		TopicDefinition: *d,
		ID:              genericTopicIDType(genericTopicID.Add(1)),
	}
}

// Topic is created by Registering a TopicDefinition on a Bus.
// You can use this to publish and subscribe events on the Bus
// for this Topic.
//
// Topics are more opinionated than using Bus methods directly,
// but you get the benefit of compile-time type checking.
//
// All Topics pass in two arguments: a context and some T.
type Topic[T any] struct {
	Bus             *Bus
	TopicDefinition TopicDefinition[T]
	TopicType       reflect.Type
	ID              genericTopicIDType
}

// Publish arg to this topic. Handlers will be called concurrently and non-deterministically.
// This will only return an error if the bus is draining.
func (t *Topic[T]) Publish(ctx context.Context, arg T) (<-chan interface{}, error) {
	return t.Bus.publish(
		t.ID,
		ctx,
		arg,
	)
}

// PublishSerially arg to this topic. Handlers will be called serially and deterministically.
// This will only return an error if the bus is draining.
func (t *Topic[T]) PublishSerially(ctx context.Context, arg T) (<-chan interface{}, error) {
	return t.Bus.publishSerially(
		t.ID,
		ctx,
		arg,
	)
}

// Subscribe a handler to this topic.
func (t *Topic[T]) Subscribe(fn func(ctx context.Context, arg T)) (<-chan interface{}, Unsubscribe) {
	return t.Bus.subscribe(
		t.ID,
		fn,
	)
}

// SubscribeOnce a handler to this topic. Once it receives an event, it will be unsubscribed.
func (t *Topic[T]) SubscribeOnce(fn func(ctx context.Context, arg T)) (<-chan interface{}, Unsubscribe) {
	return t.Bus.subscribeOnce(
		t.ID,
		fn,
	)
}
