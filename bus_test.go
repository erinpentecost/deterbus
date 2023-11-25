package deterbus

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstructor(t *testing.T) {
	b := New()
	b.Stop()
}

func TestEmptyDrain(t *testing.T) {
	b := New()
	<-b.DrainStop()
}

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

	handler := func(_ context.Context, _ int) {
		receiveCount++
	}

	dummyTopicA := NewTopic(b, TopicDefinition[int]{})

	done, _ := dummyTopicA.Subscribe(handler)
	<-done
}

func send(t testing.TB, b *Bus, syncTxn bool, handlerCount int, messageCount int) {

	// make a shared handler that just records whatever
	// int is sent to it in a slice.
	pubsLocker := &sync.Mutex{}
	pubsSeen := make([]int, 0)
	handler := func(_ context.Context, id int) {
		pubsLocker.Lock()
		pubsSeen = append(pubsSeen, id)
		pubsLocker.Unlock()
	}

	// make one topic
	topic := NewTopic(b, TopicDefinition[int]{})

	// register the handler multiple times on the topic.
	// we should get handlerCount copies of each message
	// in pubsSeen
	for i := 0; i < handlerCount; i++ {
		s, _ := topic.Subscribe(handler)
		<-s
	}

	// send a bunch of messages in any order
	ctx := context.TODO()
	var wg sync.WaitGroup
	wg.Add(messageCount)
	for i := 0; i < messageCount; i++ {
		go func(id int) {
			defer wg.Done()
			if syncTxn {
				p, err := topic.PublishSerially(ctx, id)
				require.NoError(t, err)
				<-p
			} else {
				p, err := topic.Publish(ctx, id)
				require.NoError(t, err)
				<-p
			}
		}(i)
	}
	wg.Wait() // wait until all sent

	require.Equal(t, handlerCount*messageCount, len(pubsSeen), "publishes missing")

	slices.Sort(pubsSeen)

	for h := 0; h < handlerCount; h++ {
		for m := 0; m < messageCount; m++ {
			require.Equal(t, m, pubsSeen[(h+1)*m])
		}
	}
}

func TestPublishSingleSubscriberAsync(t *testing.T) {
	b := New()
	defer b.Stop()

	send(t, b, false, 1, 2000)
}

func TestPublishSingleSubscriberSync(t *testing.T) {
	b := New()
	defer b.Stop()

	send(t, b, true, 1, 2000)
}

func TestPublishManySubscribersAsync(t *testing.T) {
	b := New()
	defer b.Stop()

	topicCount := 2000
	var wg sync.WaitGroup
	wg.Add(topicCount)

	for i := 0; i < topicCount; i++ {
		go func(topic uint64) {
			defer wg.Done()
			send(t, b, false, 1, 100)
		}(uint64(i))
	}

	wg.Wait()
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
			send(t, b, true, 1, 100)
		}(i)
	}

	wg.Wait()
}

func TestContextPublish(t *testing.T) {
	bus := New()
	defer bus.Stop()

	var foundTopic TopicIdentifier
	foundNum := uint64(4444)

	dummyTopicA := NewTopic(bus, TopicDefinition[int]{})

	expectedTopic := dummyTopicA.id.id

	handler := func(ctx context.Context, _ int) {
		foundTopic = ctx.Value(EventTopic).(TopicIdentifier)
		foundNum = ctx.Value(EventNumber).(uint64)
	}

	s, _ := dummyTopicA.Subscribe(handler)
	<-s

	p, _ := dummyTopicA.Publish(context.Background(), 4)
	<-p

	assert.Equal(t, expectedTopic, foundTopic.ID())
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

	dummyTopicA := NewTopic(bus, TopicDefinition[int]{})
	s, _ := dummyTopicA.Subscribe(func(_ context.Context, _ int) { wg.Wait() })
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

	p, _ := dummyTopicA.Publish(context.TODO(), 0)

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

func TestPublishWithNoSubscriber(t *testing.T) {
	bus := New()
	defer bus.DrainStop()

	// This should complete and not time out.
	dummyTopicA := NewTopic(bus, TopicDefinition[int]{})
	p, _ := dummyTopicA.Publish(context.Background(), 99999)
	<-p
}

type multiSubmessage struct {
	locker *sync.Mutex
	resmap *map[int]int
}

func TestPublishMultipleSubscribers(t *testing.T) {
	bus := New()

	subCount := 100
	pubCount := 400

	var resLock sync.Mutex
	res := make(map[int]int)

	dummyTopicA := NewTopic(bus, TopicDefinition[*multiSubmessage]{})

	var wg sync.WaitGroup
	wg.Add(subCount)
	for i := 0; i < subCount; i++ {
		go func(handlerid int) {
			defer wg.Done()
			s, _ := dummyTopicA.Subscribe(func(_ context.Context, msg *multiSubmessage) {
				msg.locker.Lock()
				defer msg.locker.Unlock()
				v, ok := (*msg.resmap)[handlerid]
				if ok {
					(*msg.resmap)[handlerid] = v + 1
				} else {
					(*msg.resmap)[handlerid] = 1
				}
			})
			<-s
		}(i)
	}

	wg.Wait()

	for p := 0; p < pubCount; p++ {
		d, er := dummyTopicA.Publish(context.Background(), &multiSubmessage{locker: &resLock, resmap: &res})
		assert.Nil(t, er, "publish failed")
		<-d
	}

	<-bus.DrainStop()

	for i := 0; i < subCount; i++ {
		_, ok := res[i]
		assert.True(t, ok, fmt.Sprintf("missing handler responses for handler %v", i))
	}
}

// BenchmarkSinglePublish benchmarks one publish vs one subscriber.
func BenchmarkSinglePublish(b *testing.B) {
	bus := New()
	defer bus.Stop()

	dummyTopicA := NewTopic(bus, TopicDefinition[int]{})

	handler := func(_ context.Context, _ int) {
		// no op
	}

	s, _ := dummyTopicA.Subscribe(handler)
	<-s

	for n := 0; n < b.N; n++ {
		p, _ := dummyTopicA.Publish(context.TODO(), 0)
		<-p
	}
}

func BenchmarkLoad(b *testing.B) {
	// 40 seconds
	topicCount := 50
	messagesPerTopic := 1000
	handlersPerTopic := 1000

	for n := 0; n < b.N; n++ {
		bus := New()
		defer bus.Stop()

		var wg sync.WaitGroup
		wg.Add(topicCount * messagesPerTopic * handlersPerTopic)

		topics := []*Topic[int]{}
		for i := 0; i < topicCount; i++ {
			topic := NewTopic(bus, TopicDefinition[int]{})
			topics = append(topics, topic)
			for i := 0; i < handlersPerTopic; i++ {
				s, _ := topic.Subscribe(func(_ context.Context, _ int) {
					wg.Done()
				})
				<-s
			}
		}

		for i := 0; i < topicCount; i++ {
			topic := topics[i]
			for i := 0; i < messagesPerTopic; i++ {
				topic.Publish(context.TODO(), 0)
			}
		}

		wg.Wait()
	}
}
