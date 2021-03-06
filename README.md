# deterbus

[![Go Report Card](https://goreportcard.com/badge/github.com/erinpentecost/deterbus)](https://goreportcard.com/report/github.com/erinpentecost/deterbus)
[![Travis CI](https://travis-ci.org/erinpentecost/deterbus.svg?branch=master)](https://travis-ci.org/erinpentecost/deterbus.svg?branch=master)



deterbus is a deterministic event bus for Go. Things that make this different from other event bus implementations:

* There is a single event queue for all topics. Events are processed one-at-a-time.
* Subscription and Unsubscription are events under the hood!
* An event doesn't finish processing until after all subscribed handlers have returned.
* Subscribers are invoked concurrently or serially for the same event, depending on how you publish it. The former method isn't deterministic.
* Draining the event queue is possible.
* Event publication returns a channel indicating when all subscribers have finished with that event.
* Subscribe-time handler type check to ensure handlers for the same topic have the same definitions.
* If a handler takes in a context as the first argument, the topic and published event unique id are available as values on it.

If you add a subscriber while the queue is not empty, you won't get events that should have happened before the subscriber was added. Similarly, when you unsubscribe you won't miss events that were already coming your way. This also prevents a lot of headache when you add or remove handlers from within a handler callback.

Special considerations:

* This library depends on pointers to handle callbacks instead of an entity/id system. That means serialization is a real pain.
* The event bus will keep callbacks from being garbage collected.
* Publication of one event to one subsriber isn't fast. On my system, it takes 0.184731 ms.