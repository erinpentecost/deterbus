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

	unsub := topic.Subscribe(func(_ context.Context, i int) {
		defer wg.Done()
		seen = append(seen, i)
	})

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

	unsub()
}

func TestSingleConsumerWithWait(t *testing.T) {

	b := New(Context(t))
	topic := NewTopic[int](b)

	msgCount := 5

	seen := []int{}

	unsub := topic.Subscribe(func(_ context.Context, i int) {
		seen = append(seen, i)
	})

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

	unsub()
}

func TestMultiConsumer(t *testing.T) {

	b := New(Context(t))
	topic := NewTopic[int](b)

	msgCount := 5000
	consumerCount := 100

	seen := []int{}
	wg := &sync.WaitGroup{}
	wg.Add(msgCount * consumerCount)

	unsubs := []Unsubscribe{}

	for i := 0; i < consumerCount; i++ {
		unsub := topic.Subscribe(func(_ context.Context, i int) {
			defer wg.Done()
			seen = append(seen, i)
		})
		unsubs = append(unsubs, unsub)
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

	for _, unsub := range unsubs {
		unsub()
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

			unsubs := []Unsubscribe{}

			for i := 0; i < consumerCount; i++ {
				unsub := topic.Subscribe(func(_ context.Context, i int) {
					defer wg.Done()
					seen = append(seen, i)
				})
				unsubs = append(unsubs, unsub)
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

			for _, unsub := range unsubs {
				unsub()
			}
		}(b)
	}
	topicWG.Wait()
}
