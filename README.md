# deterbus

deterbus is an opinionated, deterministic* event bus for Go. Things that make this different from other event bus implementations:

* There is a single event queue for all topics. Events are processed one-at-a-time.
* An event doesn't finish processing until after all subscribed handlers have returned.
* Subscribers are invoked concurrently for the same event.
* Subscription and Unsubscription are events under the hood!
* Draining the event queue is possible.
* Event publication returns a channel indicating when all subscribers have finished with that event.
* Doesn't work yet. :grimacing:

If you add a subscriber while the queue is not empty, you won't get events that should have happened before the subscriber was added. Similarly, when you unsubscribe you won't miss events that were already coming your way. This also prevents a lot of headache when you add or remove handlers from within a handler callback.
