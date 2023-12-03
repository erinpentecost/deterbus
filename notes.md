wow this sucks.
i could get rid of reflection if I can make real use of generics
i can't use generics of different types in the same slice (which is my single event bus)

i could get around this if each topic had its own bus.
every event gets a unique monotonic increasing uint64 event id.

i don't have to fan-in all these buses:
    i just need to call their handlers sequentially in a way that respects the event id.

if every bus has its own go routine monitoring a cond that looks at the event id,
this will work ok. each bus caller increments by 1 when it is done.
