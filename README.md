# deterbus

[![Go Report Card](https://goreportcard.com/badge/github.com/erinpentecost/deterbus)](https://goreportcard.com/report/github.com/erinpentecost/deterbus)
[![Travis CI](https://travis-ci.org/erinpentecost/deterbus.svg?branch=master)](https://travis-ci.org/erinpentecost/deterbus.svg?branch=master)



deterbus is a deterministic event bus for Go. Things that make this different from other event bus implementations:

* There is a single event queue for all topics. Events are processed one-at-a-time.
* Compile-time checks ensure your events send the types of arguments your handlers expect.
* All subscribers receive all events sent to the topic they listen to.
* Subscription and Unsubscription are events under the hood!
* An event doesn't finish processing until after all subscribed handlers have returned.
* Subscribers are invoked concurrently or serially for the same event, depending on how you publish it. The former method isn't deterministic.
* Draining the event queue is possible.
* Event publication returns a channel indicating when all subscribers have finished with that event.
* If a handler takes in a context as the first argument, the topic and published event unique id are available as values on it.

If you add a subscriber while the queue is not empty, you won't get events that should have happened before the subscriber was added. Similarly, when you unsubscribe you won't miss events that were already coming your way. This also prevents a lot of headache when you add or remove handlers from within a handler callback.

Special considerations:

* This library depends on pointers to handle callbacks instead of an entity/id system. That means serialization is a real pain.
* The event bus will keep callbacks from being garbage collected.
* Publication of one event to one subscriber isn't fast. On my system, it takes 0.184731 ms.

## Example

```go
package deterbus_test

import (
	"context"
	"testing"

	"github.com/erinpentecost/deterbus"
	"github.com/stretchr/testify/require"
)

func TestExample(t *testing.T) {
	b := deterbus.New() // Create a new Bus.
	defer b.Stop()      // Make sure we don't leave any orphaned events when we leave scope.

	// Create a new topic for tracking changes to our bank account
	// so we can maintain a balance. We specify the type of the argument
	// we are going to send along with the event with the help of generics.
	bankAccountValueChangeTopic := deterbus.NewTopic(b, deterbus.TopicDefinition[int]{})

	// Create our handler.
	balance := 0
	handlerFn := func(_ context.Context, delta int) {
		// We don't care about the context in this example.
		balance += delta
	}

	// Register a handler on the topic.
	// Subscription is an asynchronous call, so let's wait until the handler is ready by
	// calling "<-" on the returned done-ness channel.
	// If we don't wait, and instead Publish right away, the Publish may complete before
	// the Subscribe. In that case, the event would be lost.
	done, unsub := bankAccountValueChangeTopic.Subscribe(handlerFn)
	<-done // wait for subscribe to finish

	// Send a new event onto the bus.
	done, err := bankAccountValueChangeTopic.Publish(context.TODO(), 99)
	// Publication is also an asyncronous call. We can use the returned channel
	// to wait for completion just like we did for subscription.
	<-done
	require.NoError(t, err, "Publish will only return an error if the bus is draining")

	require.Equal(t, 99, balance)

	// Now we'll remove our handler and wait for it to finish.
	<-unsub()

	// Let's see what happens when we try to withdraw a million dollars now.
	done, err = bankAccountValueChangeTopic.Publish(context.TODO(), -1000000)
	<-done
	require.NoError(t, err, "Publish will only return an error if the bus is draining")

	// There's no handler, so the event was dropped and not processed by anything.
	require.Equal(t, 99, balance)
}

```
