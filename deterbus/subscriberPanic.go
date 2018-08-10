package deterbus

import (
	"fmt"
)

// SubscriberPanic wraps up panics thrown by callback functions.
type SubscriberPanic struct {
	internal      interface{}
	topic         interface{}
	publishNumber uint64
	subscriber    string
}

func (se *SubscriberPanic) Error() string {
	return fmt.Sprintf("subscriber for topic %s at %s on event number %v returned an error: %s",
		se.topic,
		se.subscriber,
		se.publishNumber,
		se.internal)
}

// Topic that the callback was handling.
func (se *SubscriberPanic) Topic() interface{} {
	return se.topic
}

// PublishNumber the callback was processing.
func (se *SubscriberPanic) PublishNumber() uint64 {
	return se.publishNumber
}

// Subscriber attach location in the source code.
func (se *SubscriberPanic) Subscriber() string {
	return se.subscriber
}
