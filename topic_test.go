package deterbus_test

import (
	"context"
	"testing"

	"github.com/erinpentecost/deterbus"
	"github.com/stretchr/testify/require"
)

func TestTopicPubSub(t *testing.T) {
	b := deterbus.New()
	defer b.Stop()

	var boolTopicDef = deterbus.TopicDefinition[bool]{}
	boolTopic := boolTopicDef.RegisterOn(b)

	found := false
	s, _ := boolTopic.Subscribe(func(ctx context.Context, arg bool) { found = arg })
	<-s

	p, err := boolTopic.Publish(context.TODO(), true)
	<-p
	require.NoError(t, err)

	require.Equal(t, true, found)
}

func TestMapTopicsPubSub(t *testing.T) {
	// This test checks that we are creating two separate
	// topics even though they have the same shape.
	b := deterbus.New()
	defer b.Stop()

	var mapTopicAlphaDef = deterbus.TopicDefinition[map[string]bool]{}
	var mapTopicBetaDef = deterbus.TopicDefinition[map[string]bool]{}
	alphaChan := mapTopicAlphaDef.RegisterOn(b)
	betaChan := mapTopicBetaDef.RegisterOn(b)

	found := map[string]bool{}

	s, _ := alphaChan.Subscribe(
		func(ctx context.Context, arg map[string]bool) {
			found["alpha"] = arg["alpha"]
		},
	)
	<-s
	s, _ = betaChan.Subscribe(
		func(ctx context.Context, arg map[string]bool) {
			found["beta"] = arg["beta"]
		},
	)
	<-s

	p, err := alphaChan.PublishSerially(context.TODO(), map[string]bool{"alpha": true})
	<-p
	require.NoError(t, err)

	p, err = betaChan.PublishSerially(context.TODO(), map[string]bool{"beta": true})
	<-p
	require.NoError(t, err)

	require.True(t, found["alpha"])
	require.True(t, found["beta"])
}
