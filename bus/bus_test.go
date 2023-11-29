package bus

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSingleConsumer(t *testing.T) {

	b := New()
	topic := NewTopic[int](b)

	msgCount := 50000

	seenMux := &sync.Mutex{}
	seen := []int{}
	wg := &sync.WaitGroup{}
	wg.Add(msgCount)

	unsub := topic.Subscribe(func(_ context.Context, i int) {
		defer wg.Done()
		seenMux.Lock()
		defer seenMux.Unlock()
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

func TestSingleMultiConsumer(t *testing.T) {

	b := New()
	topic := NewTopic[int](b)

	msgCount := 50000
	consumerCount := 100

	seenMux := &sync.Mutex{}
	seen := []int{}
	wg := &sync.WaitGroup{}
	wg.Add(msgCount * consumerCount)

	unsubs := []Unsubscribe{}

	for i := 0; i < consumerCount; i++ {
		unsub := topic.Subscribe(func(_ context.Context, i int) {
			defer wg.Done()
			seenMux.Lock()
			defer seenMux.Unlock()
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
