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

	assert.Equal(t, nil, err)
	<-done
}

/*
func TestPublish(t *testing.T) {
	b := deterbus.New()
	defer b.Stop()

	receiveCount := 0
	publishes := 500

	handler := func(ctx context.Context) {
		receiveCount++
	}

	done, err := b.Subscribe(dummyTopicA, false, handler)

	assert.Equal(t, nil, err)
	<-done

	for i := 0; i < publishes; i++ {
		<-b.Publish(context.Background(), dummyTopicA)
	}

	b.Drain()

	assert.Equal(t, publishes, receiveCount)
}
*/
