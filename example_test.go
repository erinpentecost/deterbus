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
