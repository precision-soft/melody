package main

import (
    "bytes"
    "context"
    "strings"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    rueidiscache "github.com/precision-soft/melody/integrations/rueidis/v3/cache"
    melodycache "github.com/precision-soft/melody/v3/cache"
    melodyclock "github.com/precision-soft/melody/v3/clock"
)

/* runCacheCheck exercises the rueidis cache backend against a live redis: a set/get round-trip, a
short time-to-live that actually expires the key, a miss on an absent key, and the atomic increment
counter — the behaviours a unit test with a fake backend cannot prove. */
func runCacheCheck(address string) {
    ctx := context.Background()

    provider := melodyrueidis.NewProvider()

    client, openErr := provider.Open(melodyrueidis.NewConnectionParameters(address, "", ""))
    if nil != openErr {
        fail("cache: open redis: %v", openErr)
    }
    defer client.Close()

    backend, backendErr := rueidiscache.NewBackend(client, ctx, "melody-e2e:cache:", 0, 0)
    if nil != backendErr {
        fail("cache: new backend: %v", backendErr)
    }

    key := "greeting"
    payload := []byte("hello from the cache e2e")

    /* set/get round-trip */
    if setErr := backend.SetCtx(ctx, key, payload, time.Minute); nil != setErr {
        fail("cache: set: %v", setErr)
    }

    stored, found, getErr := backend.GetCtx(ctx, key)
    if nil != getErr {
        fail("cache: get: %v", getErr)
    }
    if false == found {
        fail("cache: expected a hit for %q, got a miss", key)
    }
    if false == bytes.Equal(payload, stored) {
        fail("cache: hit payload mismatch — wanted %q, got %q", payload, stored)
    }
    pass("cache set/get round-trip on live redis (%d bytes)", len(stored))

    /* miss on an absent key */
    _, foundMissing, missErr := backend.GetCtx(ctx, "never-written")
    if nil != missErr {
        fail("cache: miss lookup: %v", missErr)
    }
    if true == foundMissing {
        fail("cache: expected a miss for an unwritten key, got a hit")
    }
    pass("cache miss reported for an absent key")

    /* a short time-to-live actually expires the key */
    expiringKey := "expiring"
    if setErr := backend.SetCtx(ctx, expiringKey, []byte("transient"), 250*time.Millisecond); nil != setErr {
        fail("cache: set expiring: %v", setErr)
    }

    time.Sleep(700 * time.Millisecond)

    _, stillThere, expiredErr := backend.GetCtx(ctx, expiringKey)
    if nil != expiredErr {
        fail("cache: expiry lookup: %v", expiredErr)
    }
    if true == stillThere {
        fail("cache: key %q survived its time-to-live", expiringKey)
    }
    pass("cache honoured the time-to-live (key expired)")

    /* atomic increment counter */
    counterKey := "counter"
    if deleteErr := backend.DeleteCtx(ctx, counterKey); nil != deleteErr {
        fail("cache: reset counter: %v", deleteErr)
    }

    first, firstErr := backend.IncrementCtx(ctx, counterKey, 3)
    if nil != firstErr {
        fail("cache: increment: %v", firstErr)
    }
    second, secondErr := backend.IncrementCtx(ctx, counterKey, 4)
    if nil != secondErr {
        fail("cache: increment: %v", secondErr)
    }
    if 3 != first || 7 != second {
        fail("cache: increment counter wrong — wanted 3 then 7, got %d then %d", first, second)
    }
    pass("cache atomic increment counter (3 → 7)")

    assertCacheKeyGrammarIsOneContract(ctx, backend)
}

/* assertCacheKeyGrammarIsOneContract is the one cache claim no unit test can settle: the Backend contract
states that a key is non-empty, carries no spaces or newlines and is at most 1024 bytes, and that EVERY
implementation refuses a malformed key with the same answer — the point being that a caller cannot tell the
backends apart by which keys they accept. A fake proves what the fake was written to say; only the in-memory
backend and a live redis, asked the same question in the same process, prove that the two agree. The payload
identity is the same kind of claim: redis has no nil to store, so an implementation preserving the
distinction would be tellable apart by reading back what was written. */
func assertCacheKeyGrammarIsOneContract(ctx context.Context, redisBackend *rueidiscache.Backend) {
    inMemory := melodycache.NewInMemoryBackend(16, time.Minute, melodyclock.NewSystemClock())
    defer inMemory.Close()

    malformedKeys := []struct {
        key      string
        expected string
    }{
        {key: "", expected: "cache key is empty"},
        {key: "with space", expected: "cache key contains spaces"},
        {key: "with\nnewline", expected: "cache key contains newlines"},
        {key: strings.Repeat("k", 1025), expected: "cache key is too long"},
    }

    for _, malformed := range malformedKeys {
        _, _, memoryErr := inMemory.Get(malformed.key)
        _, _, redisErr := redisBackend.GetCtx(ctx, malformed.key)

        if nil == memoryErr || nil == redisErr {
            fail(
                "cache: the key %q was refused by only one backend — in-memory %v, redis %v",
                malformed.key,
                memoryErr,
                redisErr,
            )
        }
        if false == strings.Contains(memoryErr.Error(), malformed.expected) {
            fail("cache: the in-memory refusal for %q was %v, wanted %q", malformed.key, memoryErr, malformed.expected)
        }
        if false == strings.Contains(redisErr.Error(), malformed.expected) {
            fail("cache: the redis refusal for %q was %v, wanted %q", malformed.key, redisErr, malformed.expected)
        }
    }
    pass("cache key grammar answered identically by the in-memory backend and by live redis (4 refusals)")

    if setErr := inMemory.Set("nil-payload", nil, time.Minute); nil != setErr {
        fail("cache: in-memory nil payload set: %v", setErr)
    }
    if setErr := redisBackend.SetCtx(ctx, "nil-payload", nil, time.Minute); nil != setErr {
        fail("cache: redis nil payload set: %v", setErr)
    }

    memoryPayload, memoryFound, memoryReadErr := inMemory.Get("nil-payload")
    if nil != memoryReadErr || false == memoryFound {
        fail("cache: in-memory nil payload read back %v / found %v", memoryReadErr, memoryFound)
    }
    redisPayload, redisFound, redisReadErr := redisBackend.GetCtx(ctx, "nil-payload")
    if nil != redisReadErr || false == redisFound {
        fail("cache: redis nil payload read back %v / found %v", redisReadErr, redisFound)
    }
    if nil == memoryPayload || 0 != len(memoryPayload) {
        fail("cache: the in-memory backend read a nil payload back as %#v, wanted an empty non-nil slice", memoryPayload)
    }
    if nil == redisPayload || 0 != len(redisPayload) {
        fail("cache: redis read a nil payload back as %#v, wanted an empty non-nil slice", redisPayload)
    }
    pass("a nil payload reads back as an empty non-nil slice from both backends")

    if deleteErr := inMemory.Delete("nil-payload"); nil != deleteErr {
        fail("cache: in-memory nil payload cleanup: %v", deleteErr)
    }
    if deleteErr := redisBackend.DeleteCtx(ctx, "nil-payload"); nil != deleteErr {
        fail("cache: redis nil payload cleanup: %v", deleteErr)
    }
}
