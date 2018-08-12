package deterbus_test

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
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

	handler := func() {
		receiveCount++
	}

	done, err := b.Subscribe(dummyTopicA, handler)
	<-done
	assert.Equal(t, nil, err)
}

func sendAsync(t *testing.T, b *deterbus.Bus, topic int, count int) {

	pubsLocker := &sync.Mutex{}
	pubsSeen := make([]int, 0)

	handler := func(id int) {
		pubsLocker.Lock()
		pubsSeen = append(pubsSeen, id)
		pubsLocker.Unlock()
	}

	s, er := b.Subscribe(topic, handler)

	assert.Equal(t, nil, er)

	<-s

	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(id int) {
			defer wg.Done()
			p, er := b.Publish(topic, id)
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

	topicCount := 2000
	var wg sync.WaitGroup
	wg.Add(topicCount)

	for i := 0; i < topicCount; i++ {
		go func(topic int) {
			defer wg.Done()
			sendAsync(t, b, topic, 100)
		}(i)
	}

	wg.Wait()
}

func TestContextPublish(t *testing.T) {
	bus := deterbus.New()
	defer bus.Stop()

	foundTopic := int(-1)
	foundNum := uint64(4444)

	expectedTopic := int(999)

	handler := func(ctx context.Context) {
		foundTopic = ctx.Value(deterbus.EventTopic).(int)
		foundNum = ctx.Value(deterbus.EventNumber).(uint64)
	}

	s, _ := bus.Subscribe(expectedTopic, handler)

	<-s

	p, _ := bus.Publish(expectedTopic, context.Background())
	<-p

	assert.Equal(t, expectedTopic, foundTopic)
	// we should be on the second publish, because
	// a publish is internally treated as a publish
	assert.Equal(t, uint64(2), foundNum)
}

func TestPanicPublish(t *testing.T) {
	bus := deterbus.New()

	expectedTopic := int(999)
	expectedPanicContent := "Oh no, I broke!"
	panicker := func() {
		panic(expectedPanicContent)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var sp deterbus.SubscriberPanic

	intercepter := func(s deterbus.SubscriberPanic) {
		sp = s
		wg.Done()
	}

	s, _ := bus.Subscribe(expectedTopic, panicker)
	p, _ := bus.SubscribeToPanic(intercepter)

	<-s
	<-p

	pub, _ := bus.Publish(expectedTopic)
	<-pub

	wg.Wait()
	bus.DrainStop()

	assert.NotEqual(t, nil, sp)
	assert.Equal(t, expectedTopic, sp.Topic())
	assert.Equal(t, expectedPanicContent, sp.Panic().(string))

	assert.True(t, strings.Contains(sp.Error(), "subscriber for topic "))
	assert.True(t, strings.Contains(sp.Subscriber(), "deterbus_test"))
}

func TestPublishWithNoSubscriber(t *testing.T) {
	bus := deterbus.New()
	defer bus.Stop()

	// This should complete and not time out.
	p, _ := bus.Publish(9999, 3423)
	<-p
}

func TestPublishMultipleSubscribers(t *testing.T) {
	bus := deterbus.New()

	subCount := 100
	pubCount := 400

	topic := 98

	var resLock sync.Mutex
	res := make(map[int]int)

	pan, _ := bus.SubscribeToPanic(func(sp deterbus.SubscriberPanic) {
		assert.FailNow(t, "callback panic")
	})
	<-pan

	var wg sync.WaitGroup
	wg.Add(subCount)
	for i := 0; i < subCount; i++ {
		go func(handlerid int) {
			defer wg.Done()
			s, er := bus.Subscribe(topic, func(locker *sync.Mutex, resmap *map[int]int) {
				locker.Lock()
				defer locker.Unlock()
				v, ok := (*resmap)[handlerid]
				if ok {
					(*resmap)[handlerid] = v + 1
				} else {
					(*resmap)[handlerid] = 1
				}
			})
			assert.Nil(t, er, "subscribe failed")
			<-s
		}(i)
	}

	wg.Wait()

	for p := 0; p < pubCount; p++ {
		d, er := bus.Publish(topic, &resLock, &res)
		assert.Nil(t, er, "publish failed")
		<-d
	}

	<-bus.DrainStop()

	for i := 0; i < subCount; i++ {
		_, ok := res[i]
		assert.True(t, ok, fmt.Sprintf("missing handler responses for handler %v", i))
	}
}

func BenchmarkAsyncPublish(b *testing.B) {
	bus := deterbus.New()
	defer bus.Stop()

	handler := func() uint64 {
		// no op
		//return ctx.Value(deterbus.EventNumber).(uint64)
		return 34
	}

	s, _ := bus.Subscribe(0, handler)

	<-s

	for n := 0; n < b.N; n++ {
		p, _ := bus.Publish(0)
		<-p
	}
}
