package deterbus_test

import (
	"context"
	"testing"

	"github.com/erinpentecost/deterbus/deterbus"
	"github.com/stretchr/testify/assert"
)

func TestConstructor(t *testing.T) {
	b := deterbus.New()
	defer b.Stop()
}

func TestEmptyDrain(t *testing.T) {
	b := deterbus.New()
	defer b.Stop()
}

type dummyTopicType int

const (
	dummyTopicA dummyTopicType = iota
)

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

/*
func TestAsyncPublish(t *testing.T) {
	b := deterbus.New()
	defer b.Stop()

	for i := 0; i < 10; i++ {
		b.Publish(context.Background(), dummyTopicA)
	}
}
*/
