# deterbus

[![Go Report Card](https://goreportcard.com/badge/github.com/erinpentecost/deterbus)](https://goreportcard.com/report/github.com/erinpentecost/deterbus)
[![Travis CI](https://travis-ci.org/erinpentecost/deterbus.svg?branch=master)](https://travis-ci.org/erinpentecost/deterbus.svg?branch=master)



deterbus is a deterministic event bus for Go. Things that make this different from other event bus implementations:

* There is a single event queue for all topics. Events are processed one-at-a-time.
* Compile-time checks ensure your events send the types of arguments your handlers expect.
* All subscribers receive all events sent to the topic they listen to.
* Subscription and Unsubscription are events under the hood!
* An event doesn't finish processing until after all subscribed handlers have returned.
* 100% test coverage.

If you add a subscriber while the queue is not empty, you won't get events that should have happened before the subscriber was added. Similarly, when you unsubscribe you won't miss events that were already coming your way. This also prevents a lot of headache when you add or remove handlers from within a handler callback.

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
	// Create a new Bus. We'll want to cancel the context we pass in eventually so we
	// don't leak a goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	b := deterbus.NewBus(ctx)
	defer cancel()

	// Create a new topic for tracking changes to our bank account
	// so we can maintain a balance. We specify the type of the argument
	// we are going to send along with the event with the help of generics.
	accountChange := deterbus.NewTopic[int](b)

	// Create our handler.
	// Callbacks are invoked one-at-a-time, so we don't need a mutex around to protect
	// the `balance` variable here.
	balance := 0
	handlerFn := func(_ context.Context, delta int) {
		balance += delta
	}

	// Register a handler on the topic.
	// Subscription is an asynchronous call. It actually just does a Publish with an
	// internal "subscribe" event. We can Wait() for that to finish or close the
	// callback with the returned CallbackManager if we ever need to.
	callbackManager := accountChange.Subscribe(handlerFn)

	// Send a new event onto the bus. We're going to wait until all callbacks have finished.
	accountChange.Publish(context.TODO(), 99).Wait()

	require.Equal(t, 99, balance)

	// Now we'll schedule the callback to be removed.
	callbackManager.Unsubscribe()

	// Let's see what happens when we try to withdraw a million dollars now.
	accountChange.Publish(context.TODO(), -1000000).Wait()

	// There's no handler, so the event was dropped and not processed by anything.
	require.Equal(t, 99, balance)
}

```
