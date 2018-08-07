package deterbus_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/erinpentecost/deterbus/deterbus"
	"github.com/stretchr/testify/assert"
)

func TestConstructor(t *testing.T) {
	b, er := deterbus.New()
	assert.Equal(t, nil, er)
	defer b.Stop()
}

func TestEmptyDrain(t *testing.T) {
	b, er := deterbus.New()
	assert.Equal(t, nil, er)
	defer b.Stop()
}

type dummyTopicType int

const (
	dummyTopicA dummyTopicType = iota
)

func TestFnType(t *testing.T) {
	sampleFn := func() bool {
		return true
	}

	assert.Equal(t, reflect.Func, reflect.TypeOf(sampleFn).Kind())
	assert.Equal(t, reflect.Func, reflect.TypeOf(TestFnType).Kind())
}

func TestSubscribe(t *testing.T) {
	b, er := deterbus.New()
	assert.Equal(t, nil, er)
	defer b.Stop()

	receiveCount := 0

	handler := func(ctx context.Context) {
		receiveCount++
	}

	done, err := b.Subscribe(dummyTopicA, false, handler)
	<-done
	assert.Equal(t, nil, err)
}

/*
func TestAsyncPublish(t *testing.T) {
	b := deterbus.New()
	defer b.Stop()

	for i := 0; i < 10; i++ {
		b.Publish(context.Background(), dummyTopicA)
	}
}
*/
