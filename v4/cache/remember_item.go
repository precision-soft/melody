package cache

import (
    "sync"

    "github.com/precision-soft/melody/v4/bound"
)

/* RememberItem is what the callback of one Remember computation says about the entry it computes: the ttl it is stored under, or that it is not stored. The zero value says nothing, so the value is stored under RememberOptions.Ttl. Remember hands every computation an item of its own and reads it once, when the callback returns without an error; a callback that fails or panics stores nothing whatever it said, and an item kept and told something after the callback returned changes no store. The methods are safe for concurrent use, and a nil item ignores what it is told and answers the zero value, so a callback can be called by hand with nil. An item is not copied once used. */
type RememberItem struct {
    mutex          sync.Mutex
    ttl            bound.Duration
    isStoreSkipped bool
}

/* SetTtl chooses the lifetime of the entry this computation stores, in place of RememberOptions.Ttl: a value of at least 1s, or unlimited for an entry kept until it is deleted or evicted. The ttl is judged when the callback returns: one below the minimum is answered by Remember as the error, naming RememberItem.Ttl, with nothing stored and no value answered. An absent ttl takes the choice back, RememberOptions.Ttl applying again. The last call wins. The configuration's ttl is the default, never a ceiling. */
func (instance *RememberItem) SetTtl(ttl bound.Duration) {
    if nil == instance {
        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.ttl = ttl
}

/* SkipStore asks that the value this computation answers is not stored: the caller and every waiter of the flight are answered it, each a copy of its own, and the next call misses and computes again. It wins over SetTtl whatever the order of the two calls, and it cannot be taken back. The value still goes through the cache's RoundTrip, so a skipped answer has the shape a stored one reads back as. */
func (instance *RememberItem) SkipStore() {
    if nil == instance {
        return
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    instance.isStoreSkipped = true
}

/* Ttl answers the ttl SetTtl chose, absent when none was chosen. */
func (instance *RememberItem) Ttl() bound.Duration {
    if nil == instance {
        return bound.Duration{}
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.ttl
}

/* IsStoreSkipped answers true once SkipStore asked that the value is not stored. */
func (instance *RememberItem) IsStoreSkipped() bool {
    if nil == instance {
        return false
    }

    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return instance.isStoreSkipped
}

/* the item read once, when the callback returned: what the store is told */
type rememberStoreChoice struct {
    ttl            bound.Duration
    isStoreSkipped bool
}

func (instance *RememberItem) storeChoice() rememberStoreChoice {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    return rememberStoreChoice{
        ttl:            instance.ttl,
        isStoreSkipped: instance.isStoreSkipped,
    }
}
