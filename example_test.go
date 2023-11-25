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

	// Create a new topic definition for tracking changes to our bank account
	// so we can maintain a balance. We specify the type of the argument
	// we are going to send along with the event with the help of generics.
	var bankAccountValueChangeTopicDefinition = deterbus.TopicDefinition[int]{}
	// Attach the topic defintion to the Bus, giving us a Topic we can publish
	// and subscribe to.
	bankAccountValueChangeTopic := bankAccountValueChangeTopicDefinition.RegisterOn(b)

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
