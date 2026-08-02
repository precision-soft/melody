package cache

import (
    "testing"
    "time"
)

/* @info an item is handed to every caller of the in-memory backend and held by the sweep at the same time, so both ends copy: the payload the constructor is given and the payload it hands back are each a copy, and so is the expiry. Nothing had ever built one — eight accessors and two defensive copies were never executed — and an aliased payload is a caller rewriting what every later reader of the same key gets, with no write path anywhere near it. */
func TestNewItem_CopiesThePayloadAndTheExpiryAtBothEnds(t *testing.T) {
    createdAt := time.Unix(100, 0)
    expiresAt := time.Unix(200, 0)
    callerPayload := []byte("payload")

    item := NewItem("app.key", callerPayload, createdAt, &expiresAt)

    callerPayload[0] = 'X'
    expiresAt = time.Unix(999, 0)

    if "payload" != string(item.Payload()) {
        t.Fatalf("expected the item to hold its own copy of the payload, got %q", string(item.Payload()))
    }

    if false == item.ExpiresAt().Equal(time.Unix(200, 0)) {
        t.Fatalf("expected the item to hold its own copy of the expiry, got %v", item.ExpiresAt())
    }

    handedOut := item.Payload()
    handedOut[0] = 'X'

    if "payload" != string(item.Payload()) {
        t.Fatalf("expected a caller's mutation of what it was handed not to reach the item, got %q", string(item.Payload()))
    }

    handedOutExpiry := item.ExpiresAt()
    *handedOutExpiry = time.Unix(999, 0)

    if false == item.ExpiresAt().Equal(time.Unix(200, 0)) {
        t.Fatalf("expected the handed-out expiry to be a copy, got %v", item.ExpiresAt())
    }

    if "app.key" != item.Key() || false == item.CreatedAt().Equal(createdAt) {
        t.Fatalf("expected the key and the creation time to be carried, got %q and %v", item.Key(), item.CreatedAt())
    }
}

/* @info an item without an expiry lives forever, and the absence has to travel as a nil rather than as a zero time — a zero time read as a deadline is a deadline already past, which would evict every immortal entry on the first sweep. */
func TestNewItem_AnItemWithoutAnExpiryCarriesNoneRatherThanAZeroTime(t *testing.T) {
    item := NewItem("app.key", nil, time.Unix(100, 0), nil)

    if nil != item.ExpiresAt() {
        t.Fatalf("expected no expiry, got %v", item.ExpiresAt())
    }

    if nil != item.Payload() {
        t.Fatalf("expected a nil payload to stay nil rather than becoming an empty slice")
    }
}

/* @info the access marks are what the eviction orders its candidates by, and the item is touched from every request goroutine that reads its key while the sweep reads the same marks — which is why they are atomics rather than plain fields. The constructor seeds the last access with the creation time so a never-read entry is not the oldest thing in the store by accident. */
func TestItem_TouchAdvancesTheAccessMarkAndCountsTheHit(t *testing.T) {
    createdAt := time.Unix(100, 0)

    item := NewItem("app.key", []byte("payload"), createdAt, nil)

    if false == item.LastAccessedAt().Equal(createdAt) {
        t.Fatalf("expected the access mark to be seeded with the creation time, got %v", item.LastAccessedAt())
    }

    if 0 != item.HitCount() {
        t.Fatalf("expected a fresh item to count no hits, got %d", item.HitCount())
    }

    item.Touch(time.Unix(150, 0))
    item.Touch(time.Unix(180, 0))

    if false == item.LastAccessedAt().Equal(time.Unix(180, 0)) {
        t.Fatalf("expected the latest access to win, got %v", item.LastAccessedAt())
    }

    if 2 != item.HitCount() {
        t.Fatalf("expected both hits to be counted, got %d", item.HitCount())
    }
}
