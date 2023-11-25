package deterbus

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConstructor(t *testing.T) {
	b := New()
	b.Stop()
}

func TestEmptyDrain(t *testing.T) {
	b := New()
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
	b := New()
	defer b.Stop()

	receiveCount := 0

	handler := func() {
		receiveCount++
	}

	done, _ := b.subscribe(dummyTopicA, handler)
	<-done
}

func send(t *testing.T, b *Bus, syncTxn bool, topic int, count int) interface{} {

	pubsLocker := &sync.Mutex{}
	pubsSeen := make([]int, 0)

	handler := func(id int) {
		pubsLocker.Lock()
		pubsSeen = append(pubsSeen, id)
		pubsLocker.Unlock()
	}

	s, _ := b.subscribe(topic, handler)

	<-s

	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(id int) {
			defer wg.Done()
			if syncTxn {
				p, er := b.publishSerially(topic, id)
				assert.Equal(t, nil, er)
				<-p
			} else {
				p, er := b.publish(topic, id)
				assert.Equal(t, nil, er)
				<-p
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, len(pubsSeen), count, "publishes missing")

	slices.Sort(pubsSeen)

	for i := 0; i < count; i++ {
		assert.Equal(t, i, pubsSeen[i])
	}

	return handler
}

func TestPublishSingleSubscriberAsync(t *testing.T) {
	b := New()
	defer b.Stop()

	send(t, b, false, 0, 2000)
}

func TestPublishManySubscribersAsync(t *testing.T) {
	b := New()
	defer b.Stop()

	topicCount := 2000
	var wg sync.WaitGroup
	wg.Add(topicCount)

	for i := 0; i < topicCount; i++ {
		go func(topic int) {
			defer wg.Done()
			send(t, b, false, topic, 100)
		}(i)
	}

	wg.Wait()
}

func TestPublishSingleSubscriberSync(t *testing.T) {
	b := New()
	defer b.Stop()

	send(t, b, true, 0, 2000)
}

func TestPublishManySubscribersSync(t *testing.T) {
	b := New()
	defer b.Stop()

	topicCount := 2000
	var wg sync.WaitGroup
	wg.Add(topicCount)

	for i := 0; i < topicCount; i++ {
		go func(topic int) {
			defer wg.Done()
			send(t, b, true, topic, 100)
		}(i)
	}

	wg.Wait()
}

func TestContextPublish(t *testing.T) {
	bus := New()
	defer bus.Stop()

	foundTopic := int(-1)
	foundNum := uint64(4444)

	expectedTopic := int(999)

	handler := func(ctx context.Context) {
		foundTopic = ctx.Value(EventTopic).(int)
		foundNum = ctx.Value(EventNumber).(uint64)
	}

	s, _ := bus.subscribe(expectedTopic, handler)

	<-s

	p, _ := bus.publish(expectedTopic, context.Background())
	<-p

	assert.Equal(t, expectedTopic, foundTopic)
	// we should be on the second publish, because
	// a subscribe is internally treated as a publish
	assert.Equal(t, uint64(2), foundNum)
}

func TestWait(t *testing.T) {
	bus := New()
	defer bus.Stop()

	<-bus.Wait() // check brand-new empty bus

	wg := sync.WaitGroup{}
	wg.Add(1)

	someTopic := int(999)
	s, _ := bus.subscribe(someTopic, func() { wg.Wait() })
	<-s

	// set up new goroutine that will wait until wg is Done,
	// then signal it's done via waitDone
	waitDone := sync.WaitGroup{}
	waitDone.Add(1)
	go func() {
		defer waitDone.Done()
		// this will hang until wg is Done()
		<-bus.Wait()
	}()

	p, _ := bus.publish(someTopic)

	// at this point, the handler is "running" trying to finish the event

	wg.Done() // finish the handler

	waitDone.Wait() // confirm the pending wait finished

	<-p // confirm the consume done signal is tripped

	<-bus.Wait() // check now-empty bus
}

type DummyInterface interface {
	Ok() bool
}

type DummyStruct struct {
	bool
}

func (d DummyStruct) Ok() bool {
	return d.bool
}

func TestPanicPublish(t *testing.T) {
	bus := New()

	expectedTopic := int(999)
	expectedPanicContent := "Oh no, I broke!"
	panicker := func() {
		panic(expectedPanicContent)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var sp SubscriberPanic

	intercepter := func(s SubscriberPanic) {
		sp = s
		wg.Done()
	}

	s, _ := bus.subscribe(expectedTopic, panicker)
	p, _ := bus.SubscribeToPanic(intercepter)

	<-s
	<-p

	pub, _ := bus.publish(expectedTopic)
	<-pub

	wg.Wait()
	bus.DrainStop()

	assert.NotEqual(t, nil, sp)
	assert.Equal(t, expectedTopic, sp.Topic())
	assert.Equal(t, expectedPanicContent, sp.Panic().(string))

	assert.True(t, strings.Contains(sp.Error(), "subscriber for topic "))
	assert.True(t, strings.Contains(sp.Subscriber(), "bus_test"))
}

func TestPublishWithNoSubscriber(t *testing.T) {
	bus := New()
	defer bus.DrainStop()

	// This should complete and not time out.
	p, _ := bus.publish(9999, 3423)
	<-p
}

func TestPublishMultipleSubscribers(t *testing.T) {
	bus := New()

	subCount := 100
	pubCount := 400

	topic := 98

	var resLock sync.Mutex
	res := make(map[int]int)

	pan, _ := bus.SubscribeToPanic(func(sp SubscriberPanic) {
		assert.FailNow(t, "callback panic")
	})
	<-pan

	var wg sync.WaitGroup
	wg.Add(subCount)
	for i := 0; i < subCount; i++ {
		go func(handlerid int) {
			defer wg.Done()
			s, _ := bus.subscribe(topic, func(locker *sync.Mutex, resmap *map[int]int) {
				locker.Lock()
				defer locker.Unlock()
				v, ok := (*resmap)[handlerid]
				if ok {
					(*resmap)[handlerid] = v + 1
				} else {
					(*resmap)[handlerid] = 1
				}
			})
			<-s
		}(i)
	}

	wg.Wait()

	for p := 0; p < pubCount; p++ {
		d, er := bus.publish(topic, &resLock, &res)
		assert.Nil(t, er, "publish failed")
		<-d
	}

	<-bus.DrainStop()

	for i := 0; i < subCount; i++ {
		_, ok := res[i]
		assert.True(t, ok, fmt.Sprintf("missing handler responses for handler %v", i))
	}
}

func simpleBenchmark(opts ...BusOption) func(b *testing.B) {
	return func(b *testing.B) {
		bus := New(opts...)
		defer bus.Stop()

		handler := func() uint64 {
			// no op
			return 34
		}

		s, _ := bus.subscribe(0, handler)

		<-s

		for n := 0; n < b.N; n++ {
			p, _ := bus.publish(0)
			<-p
		}

	}
}

// BenchmarkSinglePublish benchmarks one publish vs one subscriber.
func BenchmarkSinglePublish(b *testing.B) {
	simpleBenchmark()(b)
}
