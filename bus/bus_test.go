package bus

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func Context(t *testing.T) context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return ctx
}

func TestSingleConsumer(t *testing.T) {
	b := New(Context(t))
	topic := NewTopic[int](b)

	msgCount := 50000

	seen := []int{}
	wg := &sync.WaitGroup{}
	wg.Add(msgCount)

	cbm := topic.Subscribe(func(_ context.Context, i int) {
		defer wg.Done()
		seen = append(seen, i)
	})
	cbm.Wait()

	for i := 0; i < msgCount; i++ {
		i := i
		topic.Publish(context.TODO(), i)
	}
	wg.Wait()

	require.Len(t, seen, msgCount)

	slices.Sort(seen)
	for i := 0; i < msgCount; i++ {
		require.Equal(t, i, seen[i])
	}

	cbm.Unsubscribe()()
	// do it again just for coverage
	cbm.Unsubscribe()()
}

func TestSingleConsumerWithWait(t *testing.T) {

	b := New(Context(t))
	topic := NewTopic[int](b)

	msgCount := 5

	seen := []int{}

	cbm := topic.Subscribe(func(_ context.Context, i int) {
		seen = append(seen, i)
	})
	cbm.Wait()

	for i := 0; i < msgCount; i++ {
		i := i
		w := topic.Publish(context.TODO(), i)
		w.Wait()
	}

	require.Len(t, seen, msgCount)

	slices.Sort(seen)
	for i := 0; i < msgCount; i++ {
		require.Equal(t, i, seen[i])
	}

	unsubber := cbm.Unsubscribe()
	unsubber()
}

func TestMultiConsumer(t *testing.T) {

	b := New(Context(t))
	topic := NewTopic[int](b)

	msgCount := 5000
	consumerCount := 100

	seen := []int{}
	wg := &sync.WaitGroup{}
	wg.Add(msgCount * consumerCount)

	cbms := []CallbackManager{}

	for i := 0; i < consumerCount; i++ {
		cbm := topic.Subscribe(func(_ context.Context, i int) {
			defer wg.Done()
			seen = append(seen, i)
		})
		cbm.Wait()
		cbms = append(cbms, cbm)
	}

	for i := 0; i < msgCount; i++ {
		i := i
		topic.Publish(context.TODO(), i)
	}
	wg.Wait()

	require.Len(t, seen, msgCount*consumerCount)

	slices.Sort(seen)

	for m := 0; m < msgCount; m++ {
		for h := 0; h < consumerCount; h++ {
			idx := m*consumerCount + h
			require.Equal(t, m, seen[idx], "consumer %d, msg %d, idx %d", h, m, idx)
		}
	}

	for _, cbm := range cbms {
		cbm.Unsubscribe()
	}

}

func TestMultiTopic(t *testing.T) {

	b := New(Context(t))

	topicCount := 200
	msgCount := 1000
	consumerCount := 20

	topicWG := &sync.WaitGroup{}
	topicWG.Add(topicCount)

	syncBeforePublish := &sync.WaitGroup{}
	syncBeforePublish.Add(topicCount)
	for i := 0; i < topicCount; i++ {
		go func(b *Bus) {
			defer topicWG.Done()
			topic := NewTopic[int](b)

			seen := []int{}
			wg := &sync.WaitGroup{}
			wg.Add(msgCount * consumerCount)

			cbms := []CallbackManager{}

			for i := 0; i < consumerCount; i++ {
				unsub := topic.Subscribe(func(_ context.Context, i int) {
					defer wg.Done()
					seen = append(seen, i)
				})
				cbms = append(cbms, unsub)
			}

			// don't start publishing until all topics are ready
			syncBeforePublish.Done()
			syncBeforePublish.Wait()

			for i := 0; i < msgCount; i++ {
				i := i
				topic.Publish(context.TODO(), i)
			}
			wg.Wait()

			require.Len(t, seen, msgCount*consumerCount)

			slices.Sort(seen)

			for m := 0; m < msgCount; m++ {
				for h := 0; h < consumerCount; h++ {
					idx := m*consumerCount + h
					require.Equal(t, m, seen[idx], "consumer %d, msg %d, idx %d", h, m, idx)
				}
			}

			for _, unsub := range cbms {
				unsub.Unsubscribe()
			}
		}(b)
	}
	topicWG.Wait()
}

func TestChaining(t *testing.T) {
	msgCount := 50000
	seen := []int{}
	wg := &sync.WaitGroup{}
	wg.Add(msgCount)

	b := New(Context(t))
	topicA := NewTopic[int](b)
	topicB := NewTopic[int](b)
	topicC := NewTopic[int](b)

	cbma := topicA.Subscribe(func(ctx context.Context, i int) {
		topicB.Publish(ctx, i)
	})
	defer cbma.Unsubscribe()

	cbmb := topicB.Subscribe(func(ctx context.Context, i int) {
		topicC.Publish(ctx, i)
	})
	defer cbmb.Unsubscribe()

	cbmC := topicC.Subscribe(func(ctx context.Context, i int) {
		defer wg.Done()
		seen = append(seen, i)
	})
	defer cbmC.Unsubscribe()

	for i := 0; i < msgCount; i++ {
		i := i
		topicA.Publish(context.TODO(), i)
	}

	wg.Wait()
	require.Len(t, seen, msgCount)
	slices.Sort(seen)
	for i := 0; i < msgCount; i++ {
		require.Equal(t, i, seen[i])
	}
}

func TestSubscriptionDeterminism(t *testing.T) {
	msgCount := 500
	seen := []int{}
	wg := &sync.WaitGroup{}
	wg.Add(msgCount)

	b := New(Context(t))
	topic := NewTopic[int](b)

	type ctxKey string
	whenKey := ctxKey("when")

	// send a bunch of events before subscription
	// hold a lock on the bus to make sure we have a backlog
	contextBefore := context.WithValue(context.Background(), whenKey, -1)
	for i := 0; i < msgCount; i++ {
		topic.Publish(contextBefore, (i + 1))
	}

	cbm := topic.Subscribe(func(ctx context.Context, i int) {
		defer wg.Done()
		switch ctx.Value(whenKey).(int) {
		case -1:
			t.Fatalf("received events before subscription")
		case 0:
			seen = append(seen, i)
		case 1:
			t.Fatalf("received events after subscription")
		default:
			t.Fatalf("bad key value")
		}
	})
	cbm.Wait()

	// send events after subscription
	contextDuring := context.WithValue(context.Background(), whenKey, 0)
	for i := 0; i < msgCount; i++ {
		i := i
		topic.Publish(contextDuring, i)
	}

	cbm.Unsubscribe()()

	// send a bunch of events after unsubscription
	contextAfter := context.WithValue(context.Background(), whenKey, 1)
	for i := 0; i < 1000; i++ {
		topic.Publish(contextAfter, -10000000*(i+1))
	}

	wg.Wait()
	require.Len(t, seen, msgCount)
	slices.Sort(seen)
	for i := 0; i < msgCount; i++ {
		require.Equal(t, i, seen[i])
	}
}
