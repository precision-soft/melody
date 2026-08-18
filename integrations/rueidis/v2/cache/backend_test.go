package cache

import (
    "context"
    "errors"
    "fmt"
    "reflect"
    "strconv"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v2/exception"
    "github.com/redis/rueidis"
)

func reflectFieldNames(value any) []string {
    reflectedValue := reflect.ValueOf(value)
    if reflect.Ptr == reflectedValue.Kind() {
        reflectedValue = reflectedValue.Elem()
    }

    reflectedType := reflectedValue.Type()
    fieldNames := make([]string, 0, reflectedType.NumField())
    for fieldIndex := 0; fieldIndex < reflectedType.NumField(); fieldIndex = fieldIndex + 1 {
        fieldNames = append(fieldNames, reflectedType.Field(fieldIndex).Name)
    }

    return fieldNames
}

func TestBackendStructHasStoredCtx(t *testing.T) {
    backend := &Backend{}

    fields := reflectFieldNames(backend)
    hasCtx := false
    for _, fieldName := range fields {
        if "ctx" == fieldName {
            hasCtx = true
            break
        }
    }

    if false == hasCtx {
        t.Fatalf("Backend struct must carry a stored `ctx` field for v3.0.x source-compatibility (MEL-165)")
    }
}

func TestBackendCtxMethodsExist(t *testing.T) {
    backendType := reflect.TypeOf((*Backend)(nil))

    expected := []string{
        "GetCtx",
        "SetCtx",
        "DeleteCtx",
        "HasCtx",
        "ClearCtx",
        "ClearByPrefixCtx",
        "ManyCtx",
        "SetMultipleCtx",
        "DeleteMultipleCtx",
        "IncrementCtx",
        "DecrementCtx",
    }

    for _, methodName := range expected {
        if _, found := backendType.MethodByName(methodName); false == found {
            t.Fatalf("expected *Backend to expose %s for the ctx-first API", methodName)
        }
    }
}

func TestBackendLegacyMethodsDelegateToCtx(t *testing.T) {
    backendType := reflect.TypeOf((*Backend)(nil))

    legacy := []string{
        "Get",
        "Set",
        "Delete",
        "Has",
        "Clear",
        "ClearByPrefix",
        "Many",
        "SetMultiple",
        "DeleteMultiple",
        "Increment",
        "Decrement",
    }

    for _, methodName := range legacy {
        method, found := backendType.MethodByName(methodName)
        if false == found {
            t.Fatalf("expected *Backend to expose deprecated %s for v3.0.x source-compatibility", methodName)
        }

        for inputIndex := 1; inputIndex < method.Type.NumIn(); inputIndex = inputIndex + 1 {
            if "context.Context" == method.Type.In(inputIndex).String() {
                t.Fatalf(
                    "deprecated %s must not take a context.Context parameter (ctx is stored on Backend)",
                    methodName,
                )
            }
        }
    }
}

var (
    _ func(*Backend, context.Context, string) ([]byte, bool, error)           = (*Backend).GetCtx
    _ func(*Backend, context.Context, string, []byte, time.Duration) error    = (*Backend).SetCtx
    _ func(*Backend, context.Context, string) error                           = (*Backend).DeleteCtx
    _ func(*Backend, context.Context, string) (bool, error)                   = (*Backend).HasCtx
    _ func(*Backend, context.Context) error                                   = (*Backend).ClearCtx
    _ func(*Backend, context.Context, string) error                           = (*Backend).ClearByPrefixCtx
    _ func(*Backend, context.Context, []string) (map[string][]byte, error)    = (*Backend).ManyCtx
    _ func(*Backend, context.Context, map[string][]byte, time.Duration) error = (*Backend).SetMultipleCtx
    _ func(*Backend, context.Context, []string) error                         = (*Backend).DeleteMultipleCtx
    _ func(*Backend, context.Context, string, int64) (int64, error)           = (*Backend).IncrementCtx
    _ func(*Backend, context.Context, string, int64) (int64, error)           = (*Backend).DecrementCtx
)

var (
    _ func(*Backend, string) ([]byte, bool, error)           = (*Backend).Get
    _ func(*Backend, string, []byte, time.Duration) error    = (*Backend).Set
    _ func(*Backend, string) error                           = (*Backend).Delete
    _ func(*Backend, string) (bool, error)                   = (*Backend).Has
    _ func(*Backend) error                                   = (*Backend).Clear
    _ func(*Backend, string) error                           = (*Backend).ClearByPrefix
    _ func(*Backend, []string) (map[string][]byte, error)    = (*Backend).Many
    _ func(*Backend, map[string][]byte, time.Duration) error = (*Backend).SetMultiple
    _ func(*Backend, []string) error                         = (*Backend).DeleteMultiple
    _ func(*Backend, string, int64) (int64, error)           = (*Backend).Increment
    _ func(*Backend, string, int64) (int64, error)           = (*Backend).Decrement
)

func TestEscapeRedisGlobMeta(t *testing.T) {
    cases := []struct {
        name     string
        input    string
        expected string
    }{
        {name: "default prefix is glob-safe and unchanged", input: "melody:cache:", expected: "melody:cache:"},
        {name: "square brackets escaped", input: "user[42]:", expected: `user\[42\]:`},
        {name: "star and question mark escaped", input: "a*b?c", expected: `a\*b\?c`},
        {name: "backslash escaped", input: `back\slash`, expected: `back\\slash`},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            result := escapeRedisGlobMeta(testCase.input)
            if testCase.expected != result {
                t.Fatalf("escapeRedisGlobMeta(%q) = %q, want %q (an unescaped glob metacharacter in the literal prefix makes SCAN MATCH miss or over-match keys)", testCase.input, result, testCase.expected)
            }
        })
    }
}

type closeTrackingClient struct {
    rueidis.Client
    closed bool
}

func (instance *closeTrackingClient) Close() {
    instance.closed = true
}

func TestBackendCloseDoesNotCloseCallerOwnedClient(t *testing.T) {
    client := &closeTrackingClient{}

    backend, backendErr := NewBackend(client, nil, "", 0, 0)
    if nil != backendErr {
        t.Fatalf("NewBackend returned an error: %v", backendErr)
    }

    if closeErr := backend.Close(); nil != closeErr {
        t.Fatalf("Backend.Close returned an error: %v", closeErr)
    }

    if true == client.closed {
        t.Fatalf("Backend.Close closed the caller-owned rueidis client; the client lifecycle is owned by the provider, so the backend closing it too double-closes the shared client at shutdown")
    }
}

func TestFloorPositiveExpiry(t *testing.T) {
    cases := []struct {
        name     string
        ttl      time.Duration
        expected time.Duration
    }{
        {name: "zero stays zero (no expiry)", ttl: 0, expected: 0},
        {name: "negative is passed through unchanged, the set doors refusing it before it arrives here", ttl: -5 * time.Second, expected: -5 * time.Second},
        {name: "sub-millisecond floors to one millisecond", ttl: 500 * time.Microsecond, expected: time.Millisecond},
        {name: "one nanosecond floors to one millisecond", ttl: time.Nanosecond, expected: time.Millisecond},
        {name: "exactly one millisecond is unchanged", ttl: time.Millisecond, expected: time.Millisecond},
        {name: "above one millisecond is unchanged", ttl: 1500 * time.Millisecond, expected: 1500 * time.Millisecond},
    }

    for _, testCase := range cases {
        t.Run(testCase.name, func(t *testing.T) {
            if floored := floorPositiveExpiry(testCase.ttl); testCase.expected != floored {
                t.Fatalf("floorPositiveExpiry(%v) = %v, expected %v", testCase.ttl, floored, testCase.expected)
            }
        })
    }
}

func TestSetCtx_RefusesANegativeTtl(t *testing.T) {
    backend, client := liveBackend(t)

    setErr := backend.Set("lapsed", []byte("payload"), -time.Hour)
    if nil == setErr {
        t.Fatal("a negative ttl describes an entry that is already unreadable; storing it without expiry keeps it forever")
    }

    if false == strings.Contains(setErr.Error(), "negative") {
        t.Fatalf("expected the refusal to name the ttl, got %v", setErr)
    }

    if -2 != rawTtl(t, client, backend.prefix+"lapsed") {
        t.Fatal("the refused entry must not have been written")
    }
}

func TestSetMultipleCtx_RefusesANegativeTtl(t *testing.T) {
    backend, client := liveBackend(t)

    setErr := backend.SetMultiple(map[string][]byte{"lapsed": []byte("payload")}, -time.Hour)
    if nil == setErr {
        t.Fatal("the batch door must refuse the ttl its single sibling refuses")
    }

    if -2 != rawTtl(t, client, backend.prefix+"lapsed") {
        t.Fatal("no entry of a refused batch may have been written")
    }
}

/* zero keeps meaning no expiry on both doors, which is what separates it from the negative value refused above. */
func TestSetCtx_AZeroTtlStoresWithoutExpiry(t *testing.T) {
    backend, client := liveBackend(t)

    if setErr := backend.Set("forever", []byte("payload"), 0); nil != setErr {
        t.Fatalf("a zero ttl must be accepted: %v", setErr)
    }

    if -1 != rawTtl(t, client, backend.prefix+"forever") {
        t.Fatal("a zero ttl must store the entry without an expiry")
    }
}

func TestSetAndGetCtx_RoundTripAPayloadUnderTheConfiguredExpiry(t *testing.T) {
    backend, client := liveBackend(t)

    if setErr := backend.Set("round-trip", []byte("payload"), time.Minute); nil != setErr {
        t.Fatalf("Set: %v", setErr)
    }

    payload, found, getErr := backend.Get("round-trip")
    if nil != getErr {
        t.Fatalf("Get: %v", getErr)
    }

    if false == found {
        t.Fatal("the entry just written was not found")
    }

    if "payload" != string(payload) {
        t.Fatalf("payload = %q, want %q", string(payload), "payload")
    }

    if 0 >= rawTtl(t, client, backend.prefix+"round-trip") {
        t.Fatal("the configured expiry did not reach redis")
    }
}

func TestGetCtx_AMissingKeyIsAMissRatherThanAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    payload, found, getErr := backend.Get("absent")
    if nil != getErr {
        t.Fatalf("a miss must not be reported as a failure: %v", getErr)
    }

    if true == found {
        t.Fatal("a key that was never written must not be found")
    }

    if nil != payload {
        t.Fatalf("a miss must answer no payload, got %q", string(payload))
    }
}

func TestHasAndDeleteCtx_ReportAndRemoveOneEntry(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.Set("present", []byte("payload"), time.Minute); nil != setErr {
        t.Fatalf("Set: %v", setErr)
    }

    present, hasErr := backend.Has("present")
    if nil != hasErr || false == present {
        t.Fatalf("Has over a written entry = %v, %v", present, hasErr)
    }

    if deleteErr := backend.Delete("present"); nil != deleteErr {
        t.Fatalf("Delete: %v", deleteErr)
    }

    present, hasErr = backend.Has("present")
    if nil != hasErr || true == present {
        t.Fatalf("Has after Delete = %v, %v", present, hasErr)
    }
}

/* deleting a key that is not there is not a failure: the caller asked for its absence and gets it. */
func TestDeleteCtx_AnAbsentKeyIsNotAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    if deleteErr := backend.Delete("absent"); nil != deleteErr {
        t.Fatalf("deleting an absent key must succeed, got %v", deleteErr)
    }
}

func TestManyCtx_AnswersTheHitsAndOmitsTheMisses(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(map[string][]byte{
        "first":  []byte("one"),
        "second": []byte("two"),
    }, time.Minute); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    values, manyErr := backend.Many([]string{"first", "second", "absent"})
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 2 != len(values) {
        t.Fatalf("expected the two hits and no entry for the miss, got %v", values)
    }

    if "one" != string(values["first"]) || "two" != string(values["second"]) {
        t.Fatalf("the answer is keyed by the caller's key without the prefix, got %v", values)
    }
}

func TestManyCtx_AnEmptyKeyListIsAnEmptyAnswer(t *testing.T) {
    backend, _ := liveBackend(t)

    values, manyErr := backend.Many(nil)
    if nil != manyErr {
        t.Fatalf("an empty request must not be a failure: %v", manyErr)
    }

    if 0 != len(values) {
        t.Fatalf("expected an empty answer, got %v", values)
    }
}

func TestDeleteMultipleCtx_RemovesEveryNamedKey(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(map[string][]byte{
        "first":  []byte("one"),
        "second": []byte("two"),
    }, time.Minute); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    if deleteErr := backend.DeleteMultiple([]string{"first", "second"}); nil != deleteErr {
        t.Fatalf("DeleteMultiple: %v", deleteErr)
    }

    values, manyErr := backend.Many([]string{"first", "second"})
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 0 != len(values) {
        t.Fatalf("expected every named key to be gone, got %v", values)
    }
}

func TestDeleteMultipleCtx_AnEmptyKeyListIsNotAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    if deleteErr := backend.DeleteMultiple(nil); nil != deleteErr {
        t.Fatalf("an empty request must not be a failure: %v", deleteErr)
    }
}

func TestSetMultipleCtx_AnEmptyItemMapIsNotAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(nil, time.Minute); nil != setErr {
        t.Fatalf("an empty request must not be a failure: %v", setErr)
    }
}

func TestIncrementAndDecrementCtx_MoveOneCounter(t *testing.T) {
    backend, _ := liveBackend(t)

    value, incrementErr := backend.Increment("counter", 5)
    if nil != incrementErr {
        t.Fatalf("Increment: %v", incrementErr)
    }

    if 5 != value {
        t.Fatalf("Increment over an absent counter = %d, want 5", value)
    }

    value, decrementErr := backend.Decrement("counter", 2)
    if nil != decrementErr {
        t.Fatalf("Decrement: %v", decrementErr)
    }

    if 3 != value {
        t.Fatalf("Decrement = %d, want 3", value)
    }
}

/* redis refuses to increment a payload that is not an integer, and that refusal must reach the caller rather than be read as a fresh counter — the in-memory backend refuses the same input for the same reason. */
func TestIncrementCtx_RefusesANonIntegerPayload(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.Set("counter", []byte(""), time.Minute); nil != setErr {
        t.Fatalf("Set: %v", setErr)
    }

    value, incrementErr := backend.Increment("counter", 5)
    if nil == incrementErr {
        t.Fatalf("an empty payload is not a counter; Increment answered %d", value)
    }

    if 0 != value {
        t.Fatalf("a refused increment must answer no value, got %d", value)
    }
}

func TestClearCtx_RemovesTheWholePrefixAndLeavesTheRestAlone(t *testing.T) {
    backend, client := liveBackend(t)

    if setErr := backend.SetMultiple(map[string][]byte{
        "first":  []byte("one"),
        "second": []byte("two"),
    }, time.Minute); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    outsideKey := "melody:test:outside:" + t.Name()
    if outsideErr := client.Do(
        context.Background(),
        client.B().Set().Key(outsideKey).Value("kept").Ex(time.Minute).Build(),
    ).Error(); nil != outsideErr {
        t.Fatalf("could not write the control key: %v", outsideErr)
    }
    defer client.Do(context.Background(), client.B().Del().Key(outsideKey).Build())

    if clearErr := backend.Clear(); nil != clearErr {
        t.Fatalf("Clear: %v", clearErr)
    }

    values, manyErr := backend.Many([]string{"first", "second"})
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 0 != len(values) {
        t.Fatalf("Clear must remove the whole prefix, %v survived", values)
    }

    kept := client.Do(context.Background(), client.B().Exists().Key(outsideKey).Build())
    keptCount, _ := kept.AsInt64()
    if 1 != keptCount {
        t.Fatal("Clear must not reach beyond its own prefix")
    }
}

/* the scan pages rather than reading the keyspace in one answer, so the walk is driven past one page with a scan count of one. */
func TestClearCtx_WalksEveryPageOfTheScan(t *testing.T) {
    _, client := liveBackend(t)

    prefix := "melody:test:paged:" + t.Name() + ":"
    backend, backendErr := NewBackend(client, context.Background(), prefix, 1, 1)
    if nil != backendErr {
        t.Fatalf("NewBackend: %v", backendErr)
    }

    items := make(map[string][]byte, 12)
    for index := 0; index < 12; index = index + 1 {
        items["key-"+strconv.Itoa(index)] = []byte("payload")
    }

    if setErr := backend.SetMultiple(items, time.Minute); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    if clearErr := backend.Clear(); nil != clearErr {
        t.Fatalf("Clear: %v", clearErr)
    }

    keys := make([]string, 0, len(items))
    for key := range items {
        keys = append(keys, key)
    }

    values, manyErr := backend.Many(keys)
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 0 != len(values) {
        t.Fatalf("every page of the scan must be deleted, %d survived", len(values))
    }
}

func TestClearByPrefixCtx_RemovesOnlyTheNamedSubtree(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(map[string][]byte{
        "session:first":  []byte("one"),
        "session:second": []byte("two"),
        "profile:first":  []byte("three"),
    }, time.Minute); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    if clearErr := backend.ClearByPrefix("session:"); nil != clearErr {
        t.Fatalf("ClearByPrefix: %v", clearErr)
    }

    values, manyErr := backend.Many([]string{"session:first", "session:second", "profile:first"})
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 1 != len(values) || "three" != string(values["profile:first"]) {
        t.Fatalf("only the named subtree may be removed, got %v", values)
    }
}

/* a prefix carrying a glob metacharacter is matched literally, so it neither misses the keys it names nor reaches the siblings a wildcard would. */
func TestClearByPrefixCtx_MatchesAGlobMetacharacterLiterally(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(map[string][]byte{
        "a*b:first": []byte("one"),
        "axb:first": []byte("two"),
    }, time.Minute); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    if clearErr := backend.ClearByPrefix("a*b:"); nil != clearErr {
        t.Fatalf("ClearByPrefix: %v", clearErr)
    }

    values, manyErr := backend.Many([]string{"a*b:first", "axb:first"})
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 1 != len(values) || "two" != string(values["axb:first"]) {
        t.Fatalf("the metacharacter must match itself, not any character, got %v", values)
    }
}

func TestClearCtx_AnEmptyPrefixSpaceIsNotAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    if clearErr := backend.Clear(); nil != clearErr {
        t.Fatalf("clearing an empty prefix must succeed, got %v", clearErr)
    }
}

func TestNormalizeKey_RefusesTheSpellingsRedisCannotCarryAndNamesTheKey(t *testing.T) {
    backend := &Backend{prefix: "melody:"}

    if _, emptyErr := backend.normalizeKey(""); nil == emptyErr {
        t.Fatal("an empty key must be refused")
    }

    refusals := map[string]string{
        "spaces":   "with space",
        "newlines": "with\nnewline",
    }

    for reason, key := range refusals {
        _, err := backend.normalizeKey(key)
        if nil == err {
            t.Fatalf("%s: the key must be refused", reason)
        }

        reported, isReported := err.(*exception.Error)
        if false == isReported {
            t.Fatalf("%s: expected an exception error, got %T", reason, err)
        }

        if key != reported.Context()["key"] {
            t.Fatalf("%s: the refusal must name the key, got %v", reason, reported.Context()["key"])
        }
    }

    tooLong := strings.Repeat("k", rueidisBackendDefaultMaxKeyLength+1)
    _, longErr := backend.normalizeKey(tooLong)
    if nil == longErr {
        t.Fatal("a key above the maximum length must be refused")
    }

    normalized, normalizeErr := backend.normalizeKey("plain")
    if nil != normalizeErr {
        t.Fatalf("an ordinary key must be accepted: %v", normalizeErr)
    }

    if "melody:plain" != normalized {
        t.Fatalf("normalizeKey = %q, want the prefixed key", normalized)
    }
}

/* one malformed key stops the whole batch, so the refusal has to say which one it was — the caller handed in a set and cannot otherwise tell. */
func TestManyCtx_ABatchRefusalNamesTheOffendingKey(t *testing.T) {
    backend, _ := liveBackend(t)

    _, manyErr := backend.Many([]string{"good", "bad key", "other"})
    if nil == manyErr {
        t.Fatal("a key carrying a space must be refused")
    }

    reported, isReported := manyErr.(*exception.Error)
    if false == isReported {
        t.Fatalf("expected an exception error, got %T", manyErr)
    }

    if "bad key" != reported.Context()["key"] {
        t.Fatalf("the refusal must name the offending key, got %v", reported.Context()["key"])
    }
}

func TestDeleteKeysInBatches_DeletesEveryBatchIncludingThePartialLast(t *testing.T) {
    backend, client := liveBackend(t)

    prefix := backend.prefix
    keys := make([]string, 0, 7)
    for index := 0; index < 7; index = index + 1 {
        fullKey := prefix + "batched-" + strconv.Itoa(index)
        if setErr := client.Do(
            context.Background(),
            client.B().Set().Key(fullKey).Value("payload").Ex(time.Minute).Build(),
        ).Error(); nil != setErr {
            t.Fatalf("could not write %s: %v", fullKey, setErr)
        }

        keys = append(keys, fullKey)
    }

    backend.deleteBatch = 3

    if deleteErr := backend.deleteKeysInBatches(context.Background(), keys); nil != deleteErr {
        t.Fatalf("deleteKeysInBatches: %v", deleteErr)
    }

    for _, fullKey := range keys {
        response := client.Do(context.Background(), client.B().Exists().Key(fullKey).Build())
        count, _ := response.AsInt64()
        if 0 != count {
            t.Fatalf("%s survived the batched delete", fullKey)
        }
    }
}

func TestDeleteKeysInBatches_AnEmptyKeyListIsNotAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    if deleteErr := backend.deleteKeysInBatches(context.Background(), nil); nil != deleteErr {
        t.Fatalf("an empty batch must not be a failure: %v", deleteErr)
    }
}

func TestNewBackend_NormalizesThePrefixAndTheNonPositiveCounts(t *testing.T) {
    client := &Backend{}
    _ = client

    if _, err := NewBackend(nil, context.Background(), "", 0, 0); nil == err {
        t.Fatal("a nil client must be refused")
    }
}

func TestFirstDeleteFailure_NamesOneKeyDeterministicallyAndCountsTheRest(t *testing.T) {
    backend := &Backend{prefix: "melody:"}

    if nil != backend.firstDeleteFailure(map[string]error{"melody:a": nil, "melody:b": nil}) {
        t.Fatal("a batch in which nothing failed is not a failure")
    }

    failure := errors.New("store said no")
    reportedErr := backend.firstDeleteFailure(map[string]error{
        "melody:zulu":  failure,
        "melody:alpha": failure,
        "melody:kept":  nil,
    })

    reported, isReported := reportedErr.(*exception.Error)
    if false == isReported {
        t.Fatalf("expected an exception error, got %T", reportedErr)
    }

    if "alpha" != reported.Context()["key"] {
        t.Fatalf("the named key must be the first in order, not whichever the map yielded, got %v", reported.Context()["key"])
    }

    if 2 != reported.Context()["failedKeyCount"] {
        t.Fatalf("the caller must learn how many entries failed, got %v", reported.Context()["failedKeyCount"])
    }

    if false == errors.Is(reportedErr, failure) {
        t.Fatal("the store's own failure must stay reachable")
    }
}

/* every operation reports a store that cannot be reached rather than answering as if it had. The client is the door: rueidis refuses on a closed one with an error of its own, so this backend needs no closed flag of its own — unlike the in-memory sibling, whose map would keep answering. Driving every operation through it also enters each one's store-failure branch, which no reachable store can produce. */
func TestEveryOperation_ReportsAClosedClientInsteadOfAnsweringOverIt(t *testing.T) {
    backend, client := liveBackend(t)

    client.Close()

    if _, _, getErr := backend.Get("key"); nil == getErr {
        t.Fatal("Get over a closed client must fail")
    }

    if setErr := backend.Set("key", []byte("payload"), time.Minute); nil == setErr {
        t.Fatal("Set over a closed client must fail")
    }

    if setErr := backend.Set("key", []byte("payload"), 0); nil == setErr {
        t.Fatal("Set without an expiry over a closed client must fail")
    }

    if deleteErr := backend.Delete("key"); nil == deleteErr {
        t.Fatal("Delete over a closed client must fail")
    }

    if _, hasErr := backend.Has("key"); nil == hasErr {
        t.Fatal("Has over a closed client must fail")
    }

    if clearErr := backend.Clear(); nil == clearErr {
        t.Fatal("Clear over a closed client must fail")
    }

    if clearErr := backend.ClearByPrefix("session:"); nil == clearErr {
        t.Fatal("ClearByPrefix over a closed client must fail")
    }

    if _, manyErr := backend.Many([]string{"key"}); nil == manyErr {
        t.Fatal("Many over a closed client must fail")
    }

    if setErr := backend.SetMultiple(map[string][]byte{"key": []byte("payload")}, time.Minute); nil == setErr {
        t.Fatal("SetMultiple over a closed client must fail")
    }

    if deleteErr := backend.DeleteMultiple([]string{"key"}); nil == deleteErr {
        t.Fatal("DeleteMultiple over a closed client must fail")
    }

    if _, incrementErr := backend.Increment("counter", 1); nil == incrementErr {
        t.Fatal("Increment over a closed client must fail")
    }

    if _, decrementErr := backend.Decrement("counter", 1); nil == decrementErr {
        t.Fatal("Decrement over a closed client must fail")
    }
}

/* the batch delete names the key that stopped it, which is the whole reason the per-key answer is read rather than discarded. */
func TestDeleteMultipleCtx_AFailureNamesTheKeyAndTheBatchSize(t *testing.T) {
    backend, client := liveBackend(t)

    client.Close()

    deleteErr := backend.DeleteMultiple([]string{"zulu", "alpha"})
    if nil == deleteErr {
        t.Fatal("DeleteMultiple over a closed client must fail")
    }

    reported, isReported := deleteErr.(*exception.Error)
    if false == isReported {
        t.Fatalf("expected an exception error, got %T", deleteErr)
    }

    if "alpha" != reported.Context()["key"] {
        t.Fatalf("the failure must name a key of the batch, got %v", reported.Context()["key"])
    }

    if 2 != reported.Context()["requestedCount"] {
        t.Fatalf("the caller must learn how large the batch was, got %v", reported.Context()["requestedCount"])
    }
}

/* the batch set names the key whose write did not land: the commands are built from a map, so without the parallel key list the caller would be told only that something in the batch failed. */
func TestSetMultipleCtx_AFailureNamesOneOfTheKeys(t *testing.T) {
    backend, client := liveBackend(t)

    client.Close()

    setErr := backend.SetMultiple(map[string][]byte{"only": []byte("payload")}, time.Minute)
    if nil == setErr {
        t.Fatal("SetMultiple over a closed client must fail")
    }

    reported, isReported := setErr.(*exception.Error)
    if false == isReported {
        t.Fatalf("expected an exception error, got %T", setErr)
    }

    if "only" != reported.Context()["key"] {
        t.Fatalf("the failure must name the key, got %v", reported.Context()["key"])
    }
}

/* Close ends THIS backend and leaves the shared client alone: every later operation refuses — the answer the in-memory backend behind the same contract gives, so a teardown-ordering bug reads identically whichever backend is wired — while a second backend over the same client keeps working, which is what proves the client itself was not closed. */
func TestClose_RefusesLaterOperationsAndLeavesTheSharedClientOpen(t *testing.T) {
    backend, client := liveBackend(t)

    if closeErr := backend.Close(); nil != closeErr {
        t.Fatalf("Close: %v", closeErr)
    }

    setErr := backend.Set("after-close", []byte("payload"), time.Minute)
    if nil == setErr {
        t.Fatal("expected the closed backend to refuse")
    }

    if false == strings.Contains(setErr.Error(), "cache backend is closed") {
        t.Fatalf("expected the in-memory backend's refusal, got %v", setErr)
    }

    sibling, siblingErr := NewBackend(client, context.Background(), "melody:test:"+t.Name()+":sibling:", 0, 0)
    if nil != siblingErr {
        t.Fatalf("sibling backend: %v", siblingErr)
    }
    t.Cleanup(func() {
        _ = sibling.Clear()
    })

    if siblingSetErr := sibling.Set("after-close", []byte("payload"), time.Minute); nil != siblingSetErr {
        t.Fatalf("the shared client must stay open for its other borrowers, got %v", siblingSetErr)
    }
}

/* the empty prefix is refused like the empty key everywhere else: a prefix assembled at run time that comes out empty must not select the whole namespace. */
func TestClearByPrefix_RefusesTheEmptyPrefix(t *testing.T) {
    backend, _ := liveBackend(t)

    ctx := context.Background()

    if setErr := backend.SetCtx(ctx, "tenant:1:a", []byte("x"), time.Minute); nil != setErr {
        t.Fatalf("set: %v", setErr)
    }
    if setErr := backend.SetCtx(ctx, "other:b", []byte("y"), time.Minute); nil != setErr {
        t.Fatalf("set: %v", setErr)
    }

    clearErr := backend.ClearByPrefixCtx(ctx, "")
    if nil == clearErr {
        t.Fatal("expected the empty prefix to be refused")
    }

    if false == strings.Contains(clearErr.Error(), "cache key is empty") {
        t.Fatalf("expected the empty-key refusal, got %v", clearErr)
    }

    hasTenant, _ := backend.HasCtx(ctx, "tenant:1:a")
    hasOther, _ := backend.HasCtx(ctx, "other:b")
    if false == hasTenant || false == hasOther {
        t.Fatalf("expected the namespace to survive the refused wipe (tenant=%v other=%v)", hasTenant, hasOther)
    }
}

/* the ttl is judged before the empty early-return, the order the in-memory backend judges it in. */
func TestSetMultiple_RefusesANegativeTtlEvenForAnEmptyBatch(t *testing.T) {
    backend, _ := liveBackend(t)

    setErr := backend.SetMultipleCtx(context.Background(), map[string][]byte{}, -time.Second)
    if nil == setErr {
        t.Fatal("expected the negative ttl to be refused for the empty batch too")
    }

    if false == strings.Contains(setErr.Error(), "cache ttl is negative") {
        t.Fatalf("expected the negative ttl refusal, got %v", setErr)
    }
}

/* the counter refusal names the key it happened on, the way the in-memory backend's does. */
func TestIncrement_WrapsTheStoreRefusalWithTheKey(t *testing.T) {
    backend, _ := liveBackend(t)

    ctx := context.Background()

    if setErr := backend.SetCtx(ctx, "counter", []byte("not-a-number"), time.Minute); nil != setErr {
        t.Fatalf("set: %v", setErr)
    }

    _, incrementErr := backend.IncrementCtx(ctx, "counter", 1)
    if nil == incrementErr {
        t.Fatal("expected the non-numeric payload to refuse the increment")
    }

    reported, isReported := incrementErr.(*exception.Error)
    if false == isReported {
        t.Fatalf("expected an exception error naming the key, got %T: %v", incrementErr, incrementErr)
    }

    if "counter" != reported.Context()["key"] {
        t.Fatalf("expected the refusal to name the key, got %v", reported.Context()["key"])
    }
}

/* the configured command timeout bounds the contract calls, which carry no context of their own. */
func TestCommandTimeout_BoundsTheContractCalls(t *testing.T) {
    _, client := liveBackend(t)

    bounded, boundedErr := NewBackendWithCommandTimeout(client, context.Background(), "melody:test:"+t.Name()+":", 0, 0, time.Nanosecond)
    if nil != boundedErr {
        t.Fatalf("backend: %v", boundedErr)
    }

    _, _, getErr := bounded.Get("any")
    if nil == getErr {
        t.Fatal("expected the one-nanosecond bound to cut the call")
    }

    if false == strings.Contains(getErr.Error(), "context deadline exceeded") {
        t.Fatalf("expected the deadline to be the cause, got %v", getErr)
    }
}

func TestFirstSetFailure_NamesTheSortedFirstKeyAndCountsTheRest(t *testing.T) {
    backend := &Backend{prefix: "p:"}

    firstErr := errors.New("first")
    zebraErr := errors.New("zebra")

    reported := backend.firstSetFailure(map[string]error{
        "zebra": zebraErr,
        "alpha": firstErr,
    }, 5)

    if nil == reported {
        t.Fatal("expected a failure report")
    }

    melodyErr, isMelodyErr := reported.(*exception.Error)
    if false == isMelodyErr {
        t.Fatalf("expected a melody error, got %T", reported)
    }

    contextValue := melodyErr.Context()
    if "alpha" != contextValue["key"] {
        t.Fatalf("expected the sorted-first key reported, got %v", contextValue["key"])
    }
    if 2 != contextValue["failedKeyCount"] || 5 != contextValue["requestedCount"] {
        t.Fatalf("expected the counts to describe the batch, got %#v", contextValue)
    }

    if false == errors.Is(reported, firstErr) {
        t.Fatalf("expected the sorted-first key's own error as the cause")
    }

    if nil != backend.firstSetFailure(map[string]error{}, 3) {
        t.Fatalf("expected an empty failure map to answer nil")
    }
}

/* the validation refusal names the sorted-first malformed key, never a map-iteration choice — the nondeterminism the response reporting further down already refuses. Validation runs before anything is sent, so the batch leaves no trace. */
func TestSetMultipleCtx_NamesTheSortedFirstMalformedKey(t *testing.T) {
    backend, _ := liveBackend(t)

    items := map[string][]byte{
        "zz bad": []byte("z"),
        "aa bad": []byte("a"),
        "good":   []byte("g"),
    }

    for attempt := 0; attempt < 8; attempt++ {
        setErr := backend.SetMultiple(items, 0)
        if nil == setErr {
            t.Fatal("expected the malformed keys to be refused")
        }

        namedKey := exception.LogContext(setErr)["key"]
        if "aa bad" != namedKey {
            t.Fatalf("expected the sorted-first malformed key named, got %v", namedKey)
        }
    }
}

/* a multi-batch wipe that fails part-way answers at the operation's extent — the batches before the failure are irreversibly gone, and the batch's own counts could not say how much of the namespace went with them. A single-batch operation keeps the batch report, whose counts already are the operation's. */
func TestDeleteKeysInBatches_AMultiBatchFailureReportsTheOperationsExtent(t *testing.T) {
    backend, _ := liveBackend(t)

    keyCount := backend.deleteBatch + 10
    keys := make([]string, 0, keyCount)
    for index := 0; index < keyCount; index++ {
        keys = append(keys, fmt.Sprintf("%sextent-%05d", backend.prefix, index))
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    deleteErr := backend.deleteKeysInBatches(cancelledContext, keys)
    if nil == deleteErr {
        t.Fatal("expected the cancelled batch walk to fail")
    }

    extent := exception.LogContext(deleteErr)
    if nil == extent["deletedBeforeFailure"] || nil == extent["requestedTotal"] {
        t.Fatalf("expected the operation-level extent on the error, got %v", extent)
    }

    singleBatchErr := backend.deleteKeysInBatches(cancelledContext, keys[:1])
    if nil == singleBatchErr {
        t.Fatal("expected the cancelled single batch to fail")
    }

    if nil != exception.LogContext(singleBatchErr)["deletedBeforeFailure"] {
        t.Fatal("expected the single-batch operation to keep the batch report as its own")
    }
}

/* the caller's own mistakes are named the way the in-memory backend names them, because the shared contract makes the grammar of a refusal part of the promise. Three distinct mistakes used to arrive under one message that is also the message of a store outage, so neither the operator nor the application could tell a bug in the call from redis being down. Redis refuses all three itself — what was missing was never the refusal but its name. */
func TestBackend_CounterRefusalsAreNamedTheWayTheInMemorySiblingNamesThem(t *testing.T) {
    for _, testCase := range []struct {
        name    string
        cause   error
        message string
    }{
        {
            name:    "a delta that overflows when negated",
            cause:   errors.New("ERR decrement would overflow"),
            message: "delta overflows int64 when negated",
        },
        {
            name:    "a counter driven past the int64 ceiling",
            cause:   errors.New("ERR increment or decrement would overflow"),
            message: "cache increment overflow",
        },
        {
            name:    "a payload that is not a canonical number",
            cause:   errors.New("ERR value is not an integer or out of range"),
            message: "cache value is not a valid int64",
        },
        {
            name:    "anything else is the store failing, and now really means it",
            cause:   errors.New("LOADING Redis is loading the dataset in memory"),
            message: "cache counter operation failed",
        },
        {
            name:    "no cause at all stays on the generic message",
            cause:   nil,
            message: "cache counter operation failed",
        },
    } {
        t.Run(testCase.name, func(t *testing.T) {
            refusal := counterError("probe-key", testCase.cause)

            if testCase.message != refusal.Error() {
                t.Fatalf("message = %q, want %q", refusal.Error(), testCase.message)
            }

            if nil != testCase.cause && false == errors.Is(refusal, testCase.cause) {
                t.Fatal("the redis error must survive as the cause in every branch")
            }
        })
    }
}

/* the fragments are matched inside whatever prefix the server or a script wraps them in, and case does not decide */
func TestBackend_CounterRefusalsAreMatchedInsideTheServersOwnWrapping(t *testing.T) {
    refusal := counterError(
        "probe-key",
        errors.New("ERR Error running script (call to f_abc): @user_script:4: VALUE IS NOT AN INTEGER OR OUT OF RANGE"),
    )

    if "cache value is not a valid int64" != refusal.Error() {
        t.Fatalf("message = %q, want the payload refusal", refusal.Error())
    }
}
