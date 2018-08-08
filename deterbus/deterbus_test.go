package deterbus_test

import (
	"context"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/erinpentecost/deterbus/deterbus"
	"github.com/stretchr/testify/assert"
)

func TestConstructor(t *testing.T) {
	b := deterbus.New()
	b.Stop()
}

func TestEmptyDrain(t *testing.T) {
	b := deterbus.New()
	<-b.DrainStop()
}

type dummyTopicType int

const (
	dummyTopicA dummyTopicType = iota
	dummyTopicB
)

func TestFnType(t *testing.T) {
	sampleFn := func() bool {
		return true
	}

	assert.Equal(t, reflect.Func, reflect.TypeOf(sampleFn).Kind())
	assert.Equal(t, reflect.Func, reflect.TypeOf(TestFnType).Kind())
}

func TestSubscribe(t *testing.T) {
	b := deterbus.New()
	defer b.Stop()

	receiveCount := 0

	handler := func(ctx context.Context) {
		receiveCount++
	}

	done, err := b.Subscribe(dummyTopicA, false, handler)
	<-done
	assert.Equal(t, nil, err)
}

func sendAsync(t *testing.T, b *deterbus.Bus, topic int, count int) {

	pubsLocker := &sync.Mutex{}
	pubsSeen := make([]int, 0)

	handler := func(ctx context.Context, id int) {
		pubsLocker.Lock()
		pubsSeen = append(pubsSeen, id)
		pubsLocker.Unlock()
	}

	s, er := b.Subscribe(topic, false, handler)

	assert.Equal(t, nil, er)

	<-s

	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(id int) {
			defer wg.Done()
			p, er := b.Publish(context.Background(), topic, id)
			assert.Equal(t, nil, er)
			<-p
		}(i)
	}

	wg.Wait()

	assert.Equal(t, len(pubsSeen), count, "publishes missing")

	sort.Ints(pubsSeen)

	for i := 0; i < count; i++ {
		assert.Equal(t, i, pubsSeen[i])
	}
}

func TestAsyncPublishSingleSubscriber(t *testing.T) {
	b := deterbus.New()
	defer b.Stop()

	sendAsync(t, b, 0, 2000)
}

func TestAsyncPublishManySubscribers(t *testing.T) {
	b := deterbus.New()
	defer b.Stop()

	topicCount := 1000
	var wg sync.WaitGroup
	wg.Add(topicCount)

	for i := 0; i < topicCount; i++ {
		go func(topic int) {
			defer wg.Done()
			sendAsync(t, b, topic, 5)
		}(i)
	}

	wg.Wait()
}
