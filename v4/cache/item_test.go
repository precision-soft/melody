package cache

import (
    "testing"
    "time"
)

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

func TestNewItem_AnItemWithoutAnExpiryCarriesNoneRatherThanAZeroTime(t *testing.T) {
    item := NewItem("app.key", nil, time.Unix(100, 0), nil)

    if nil != item.ExpiresAt() {
        t.Fatalf("expected no expiry, got %v", item.ExpiresAt())
    }

    if nil != item.Payload() {
        t.Fatalf("expected a nil payload to stay nil rather than becoming an empty slice")
    }
}

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
