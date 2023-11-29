package deterbus

import (
	"context"
	"reflect"
	"sync/atomic"
)

// TopicIDType uniquely identifies a topic.
type TopicIDType uint64

var genericTopicID atomic.Uint64

// TopicDefinition is a definition for a topic.
// It exists to hold the type T.
type TopicDefinition[T any] struct {
	// Name can be set to provide a user-readable name.
	// This might appear in logs.
	Name string
}

// topicIdentifier is a container for the ID and a Name of a topic.
type topicIdentifier struct {
	id   TopicIDType
	name string
}

type TopicIdentifier interface {
	ID() TopicIDType
	Name() string
}

func (ti *topicIdentifier) ID() TopicIDType {
	return ti.id
}

func (ti *topicIdentifier) Name() string {
	return ti.name
}

var _ TopicIdentifier = (*topicIdentifier)(nil)

// RegisterOn registers a TopicDefinition onto a Bus to get a Topic.
// You probably just want to use NewTopic().
func (d *TopicDefinition[T]) RegisterOn(bus *Bus) *Topic[T] {
	return &Topic[T]{
		Bus:             bus,
		TopicDefinition: *d,
		id: &topicIdentifier{
			id:   TopicIDType(genericTopicID.Add(1)),
			name: d.Name,
		},
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
	id              *topicIdentifier
}

// NewTopic registers a TopicDefinition onto a Bus to get a Topic.
func NewTopic[T any](bus *Bus, d TopicDefinition[T]) *Topic[T] {
	return d.RegisterOn(bus)
}

// Publish arg to this topic. Handlers will be called concurrently and non-deterministically.
// This will only return an error if the bus is draining.
func (t *Topic[T]) Publish(ctx context.Context, arg T) (<-chan interface{}, error) {
	return t.Bus.publish(
		t.id,
		ctx,
		arg,
	)
}

// PublishSerially arg to this topic. Handlers will be called serially and deterministically.
// This will only return an error if the bus is draining.
func (t *Topic[T]) PublishSerially(ctx context.Context, arg T) (<-chan interface{}, error) {
	return t.Bus.publishSerially(
		t.id,
		ctx,
		arg,
	)
}

// Subscribe a handler to this topic.
func (t *Topic[T]) Subscribe(fn func(ctx context.Context, arg T)) (<-chan interface{}, Unsubscribe) {
	var zeroArg [0]T
	return t.Bus.subscribe(
		t.id,
		fn,
		reflect.TypeOf(zeroArg),
	)
}

// SubscribeOnce a handler to this topic. Once it receives an event, it will be unsubscribed.
func (t *Topic[T]) SubscribeOnce(fn func(ctx context.Context, arg T)) (<-chan interface{}, Unsubscribe) {
	var zeroArg [0]T
	return t.Bus.subscribeOnce(
		t.id,
		fn,
		reflect.TypeOf(zeroArg),
	)
}
