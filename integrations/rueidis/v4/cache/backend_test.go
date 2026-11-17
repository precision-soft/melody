package cache

import (
    "context"
    "errors"
    "fmt"
    "math"
    "reflect"
    "strconv"
    "strings"
    "sync"
    "sync/atomic"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    cachecontract "github.com/precision-soft/melody/v4/cache/contract"
    "github.com/precision-soft/melody/v4/exception"
    "github.com/redis/rueidis"
)

func TestBackend_EveryDoorIsContextFirstWithAPlainName(t *testing.T) {
    backendType := reflect.TypeOf((*Backend)(nil))

    for _, methodName := range []string{"Get", "Set", "Delete", "Has", "Clear", "ClearByPrefix", "Many", "SetMultiple", "DeleteMultiple", "Increment", "Decrement"} {
        method, found := backendType.MethodByName(methodName)
        if false == found {
            t.Fatalf("expected *Backend to expose %s", methodName)
        }

        if 2 > method.Type.NumIn() || "context.Context" != method.Type.In(1).String() {
            t.Fatalf("expected %s to take a context.Context first", methodName)
        }

        if _, doubleFound := backendType.MethodByName(methodName + "Context"); true == doubleFound {
            t.Fatalf("expected no %sContext double beside %s", methodName, methodName)
        }
    }
}

var (
    _ func(*Backend, context.Context, string) ([]byte, bool, error)            = (*Backend).Get
    _ func(*Backend, context.Context, string, []byte, bound.Duration) error    = (*Backend).Set
    _ func(*Backend, context.Context, string) error                            = (*Backend).Delete
    _ func(*Backend, context.Context, string) (bool, error)                    = (*Backend).Has
    _ func(*Backend, context.Context) error                                    = (*Backend).Clear
    _ func(*Backend, context.Context, string) error                            = (*Backend).ClearByPrefix
    _ func(*Backend, context.Context, []string) (map[string][]byte, error)     = (*Backend).Many
    _ func(*Backend, context.Context, map[string][]byte, bound.Duration) error = (*Backend).SetMultiple
    _ func(*Backend, context.Context, []string) error                          = (*Backend).DeleteMultiple
    _ func(*Backend, context.Context, string, int64, cachecontract.CounterOptions) (int64, error) = (*Backend).Increment
    _ func(*Backend, context.Context, string, int64, cachecontract.CounterOptions) (int64, error) = (*Backend).Decrement
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

    backend, backendErr := NewBackend(client, BackendConfig{})
    if nil != backendErr {
        t.Fatalf("NewBackend returned an error: %v", backendErr)
    }

    if closeErr := backend.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("Backend.Close returned an error: %v", closeErr)
    }

    if true == client.closed {
        t.Fatalf("Backend.Close closed the caller-owned rueidis client; the client lifecycle is owned by the provider, so the backend closing it too double-closes the shared client at shutdown")
    }
}

func TestSet_RefusesAnAbsentTtlAndOneBelowItsMinimum(t *testing.T) {
    backend, client := liveBackend(t)

    for _, refusedTtl := range []bound.Duration{{}, bound.Of(time.Duration(0)), bound.Of(-time.Hour), bound.Of(time.Second - time.Nanosecond)} {
        setErr := backend.Set(context.Background(), "lapsed", []byte("payload"), refusedTtl)
        if nil == setErr || "ttl" != exception.LogContext(setErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("expected the ttl %s refused naming it, got %v", refusedTtl, setErr)
        }
    }

    if -2 != rawTtl(t, client, backend.prefix+"lapsed") {
        t.Fatal("the refused entry must not have been written")
    }
}

func TestSetMultiple_RefusesAnAbsentTtlAndOneBelowItsMinimum(t *testing.T) {
    backend, client := liveBackend(t)

    for _, refusedTtl := range []bound.Duration{{}, bound.Of(time.Duration(0)), bound.Of(-time.Hour), bound.Of(time.Second - time.Nanosecond)} {
        setErr := backend.SetMultiple(context.Background(), map[string][]byte{"lapsed": []byte("payload")}, refusedTtl)
        if nil == setErr || "ttl" != exception.LogContext(setErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("the batch door must refuse the ttl %s its single sibling refuses, got %v", refusedTtl, setErr)
        }
    }

    if -2 != rawTtl(t, client, backend.prefix+"lapsed") {
        t.Fatal("no entry of a refused batch may have been written")
    }
}

func TestSet_AnUnlimitedTtlStoresWithoutExpiry(t *testing.T) {
    backend, client := liveBackend(t)

    if setErr := backend.Set(context.Background(), "forever", []byte("payload"), bound.UnlimitedDuration()); nil != setErr {
        t.Fatalf("an unlimited ttl must be accepted: %v", setErr)
    }

    if -1 != rawTtl(t, client, backend.prefix+"forever") {
        t.Fatal("an unlimited ttl must store the entry without an expiry")
    }
}

func TestSetAndGet_RoundTripAPayloadUnderTheConfiguredExpiry(t *testing.T) {
    backend, client := liveBackend(t)

    if setErr := backend.Set(context.Background(), "round-trip", []byte("payload"), bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("Set: %v", setErr)
    }

    payload, found, getErr := backend.Get(context.Background(), "round-trip")
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

func TestGet_AMissingKeyIsAMissRatherThanAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    payload, found, getErr := backend.Get(context.Background(), "absent")
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

func TestHasAndDelete_ReportAndRemoveOneEntry(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.Set(context.Background(), "present", []byte("payload"), bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("Set: %v", setErr)
    }

    present, hasErr := backend.Has(context.Background(), "present")
    if nil != hasErr || false == present {
        t.Fatalf("Has over a written entry = %v, %v", present, hasErr)
    }

    if deleteErr := backend.Delete(context.Background(), "present"); nil != deleteErr {
        t.Fatalf("Delete: %v", deleteErr)
    }

    present, hasErr = backend.Has(context.Background(), "present")
    if nil != hasErr || true == present {
        t.Fatalf("Has after Delete = %v, %v", present, hasErr)
    }
}

func TestDelete_AnAbsentKeyIsNotAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    if deleteErr := backend.Delete(context.Background(), "absent"); nil != deleteErr {
        t.Fatalf("deleting an absent key must succeed, got %v", deleteErr)
    }
}

func TestMany_AnswersTheHitsAndOmitsTheMisses(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(context.Background(), map[string][]byte{
        "first":  []byte("one"),
        "second": []byte("two"),
    }, bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    values, manyErr := backend.Many(context.Background(), []string{"first", "second", "absent"})
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

func TestMany_AnEmptyKeyListIsAnEmptyAnswer(t *testing.T) {
    backend, _ := liveBackend(t)

    values, manyErr := backend.Many(context.Background(), nil)
    if nil != manyErr {
        t.Fatalf("an empty request must not be a failure: %v", manyErr)
    }

    if 0 != len(values) {
        t.Fatalf("expected an empty answer, got %v", values)
    }
}

func TestDeleteMultiple_RemovesEveryNamedKey(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(context.Background(), map[string][]byte{
        "first":  []byte("one"),
        "second": []byte("two"),
    }, bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    if deleteErr := backend.DeleteMultiple(context.Background(), []string{"first", "second"}); nil != deleteErr {
        t.Fatalf("DeleteMultiple: %v", deleteErr)
    }

    values, manyErr := backend.Many(context.Background(), []string{"first", "second"})
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 0 != len(values) {
        t.Fatalf("expected every named key to be gone, got %v", values)
    }
}

func TestDeleteMultiple_AnEmptyKeyListIsNotAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    if deleteErr := backend.DeleteMultiple(context.Background(), nil); nil != deleteErr {
        t.Fatalf("an empty request must not be a failure: %v", deleteErr)
    }
}

func TestSetMultiple_AnEmptyItemMapIsNotAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(context.Background(), nil, bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("an empty request must not be a failure: %v", setErr)
    }
}

func TestIncrementAndDecrement_MoveOneCounter(t *testing.T) {
    backend, _ := liveBackend(t)

    value, incrementErr := backend.Increment(context.Background(), "counter", 5, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != incrementErr {
        t.Fatalf("Increment: %v", incrementErr)
    }

    if 5 != value {
        t.Fatalf("Increment over an absent counter = %d, want 5", value)
    }

    value, decrementErr := backend.Decrement(context.Background(), "counter", 2, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil != decrementErr {
        t.Fatalf("Decrement: %v", decrementErr)
    }

    if 3 != value {
        t.Fatalf("Decrement = %d, want 3", value)
    }
}

func TestIncrement_RefusesANonIntegerPayload(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.Set(context.Background(), "counter", []byte(""), bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("Set: %v", setErr)
    }

    value, incrementErr := backend.Increment(context.Background(), "counter", 5, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil == incrementErr {
        t.Fatalf("an empty payload is not a counter; Increment answered %d", value)
    }

    if 0 != value {
        t.Fatalf("a refused increment must answer no value, got %d", value)
    }
}

func TestClear_RemovesTheWholePrefixAndLeavesTheRestAlone(t *testing.T) {
    backend, client := liveBackend(t)

    if setErr := backend.SetMultiple(context.Background(), map[string][]byte{
        "first":  []byte("one"),
        "second": []byte("two"),
    }, bound.Of(time.Minute)); nil != setErr {
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

    if clearErr := backend.Clear(context.Background()); nil != clearErr {
        t.Fatalf("Clear: %v", clearErr)
    }

    values, manyErr := backend.Many(context.Background(), []string{"first", "second"})
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

func TestClear_WalksEveryPageOfTheScan(t *testing.T) {
    _, client := liveBackend(t)

    prefix := "melody:test:paged:" + t.Name() + ":"
    backend, backendErr := NewBackend(client, BackendConfig{Prefix: prefix, ScanCount: bound.Of(1), DeleteBatch: bound.Of(1)})
    if nil != backendErr {
        t.Fatalf("NewBackend: %v", backendErr)
    }

    items := make(map[string][]byte, 12)
    for index := 0; index < 12; index = index + 1 {
        items["key-"+strconv.Itoa(index)] = []byte("payload")
    }

    if setErr := backend.SetMultiple(context.Background(), items, bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    if clearErr := backend.Clear(context.Background()); nil != clearErr {
        t.Fatalf("Clear: %v", clearErr)
    }

    keys := make([]string, 0, len(items))
    for key := range items {
        keys = append(keys, key)
    }

    values, manyErr := backend.Many(context.Background(), keys)
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 0 != len(values) {
        t.Fatalf("every page of the scan must be deleted, %d survived", len(values))
    }
}

func TestClearByPrefix_RemovesOnlyTheNamedSubtree(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(context.Background(), map[string][]byte{
        "session:first":  []byte("one"),
        "session:second": []byte("two"),
        "profile:first":  []byte("three"),
    }, bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    if clearErr := backend.ClearByPrefix(context.Background(), "session:"); nil != clearErr {
        t.Fatalf("ClearByPrefix: %v", clearErr)
    }

    values, manyErr := backend.Many(context.Background(), []string{"session:first", "session:second", "profile:first"})
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 1 != len(values) || "three" != string(values["profile:first"]) {
        t.Fatalf("only the named subtree may be removed, got %v", values)
    }
}

func TestClearByPrefix_MatchesAGlobMetacharacterLiterally(t *testing.T) {
    backend, _ := liveBackend(t)

    if setErr := backend.SetMultiple(context.Background(), map[string][]byte{
        "a*b:first": []byte("one"),
        "axb:first": []byte("two"),
    }, bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    if clearErr := backend.ClearByPrefix(context.Background(), "a*b:"); nil != clearErr {
        t.Fatalf("ClearByPrefix: %v", clearErr)
    }

    values, manyErr := backend.Many(context.Background(), []string{"a*b:first", "axb:first"})
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 1 != len(values) || "two" != string(values["axb:first"]) {
        t.Fatalf("the metacharacter must match itself, not any character, got %v", values)
    }
}

func TestClear_AnEmptyPrefixSpaceIsNotAFailure(t *testing.T) {
    backend, _ := liveBackend(t)

    if clearErr := backend.Clear(context.Background()); nil != clearErr {
        t.Fatalf("clearing an empty prefix must succeed, got %v", clearErr)
    }
}

func TestNormalizeKey_RefusesTheSpellingsRedisCannotCarryAndNamesTheKey(t *testing.T) {
    backend := &Backend{prefix: "melody:", maxKeyLength: backendMaxKeyLengthRule.Default}

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

    tooLong := strings.Repeat("k", backendMaxKeyLengthRule.Default+1)
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

func TestMany_ABatchRefusalNamesTheOffendingKey(t *testing.T) {
    backend, _ := liveBackend(t)

    _, manyErr := backend.Many(context.Background(), []string{"good", "bad key", "other"})
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

    if deleteErr := backend.deleteKeysInBatches(backend.walkRoundTrip(context.Background(), "Clear"), keys); nil != deleteErr {
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

    if deleteErr := backend.deleteKeysInBatches(backend.walkRoundTrip(context.Background(), "Clear"), nil); nil != deleteErr {
        t.Fatalf("an empty batch must not be a failure: %v", deleteErr)
    }
}

func TestNewBackend_RefusesANilClient(t *testing.T) {
    if _, err := NewBackend(nil, BackendConfig{}); nil == err {
        t.Fatal("a nil client must be refused")
    }
}

func TestNewBackend_AnAbsentConfigTakesTheDefaults(t *testing.T) {
    backend, backendErr := NewBackend(&closeTrackingClient{}, BackendConfig{})
    if nil != backendErr {
        t.Fatalf("NewBackend returned an error: %v", backendErr)
    }

    if defaultBackendPrefix != backend.prefix || 1000 != backend.scanCount || 500 != backend.deleteBatch || 1024 != backend.maxKeyLength || time.Second != backend.commandTimeout {
        t.Fatalf("expected the default prefix, 1000, 500, 1024 and 1s, got %q, %d, %d, %d, %v", backend.prefix, backend.scanCount, backend.deleteBatch, backend.maxKeyLength, backend.commandTimeout)
    }
}

func TestNewBackend_RefusesALimitOutsideItsRuleNamingTheField(t *testing.T) {
    cases := map[string]struct {
        config BackendConfig
        field  string
    }{
        "zero scan count":                 {config: BackendConfig{ScanCount: bound.Of(0)}, field: "BackendConfig.ScanCount"},
        "unlimited scan count":            {config: BackendConfig{ScanCount: bound.UnlimitedCount()}, field: "BackendConfig.ScanCount"},
        "zero delete batch":               {config: BackendConfig{DeleteBatch: bound.Of(0)}, field: "BackendConfig.DeleteBatch"},
        "unlimited delete batch":          {config: BackendConfig{DeleteBatch: bound.UnlimitedCount()}, field: "BackendConfig.DeleteBatch"},
        "zero max key length":             {config: BackendConfig{MaxKeyLength: bound.Of(0)}, field: "BackendConfig.MaxKeyLength"},
        "unlimited max key length":        {config: BackendConfig{MaxKeyLength: bound.UnlimitedCount()}, field: "BackendConfig.MaxKeyLength"},
        "sub-millisecond command timeout": {config: BackendConfig{CommandTimeout: bound.Of(time.Microsecond)}, field: "BackendConfig.CommandTimeout"},
        "unlimited command timeout":       {config: BackendConfig{CommandTimeout: bound.UnlimitedDuration()}, field: "BackendConfig.CommandTimeout"},
    }

    for name, testCase := range cases {
        t.Run(name, func(t *testing.T) {
            _, backendErr := NewBackend(&closeTrackingClient{}, testCase.config)
            if nil == backendErr {
                t.Fatalf("expected a refusal naming %s, got none", testCase.field)
            }

            if testCase.field != exception.LogContext(backendErr, exception.LogContextConfig{})["field"] {
                t.Fatalf("expected the refusal to name %s, got %v", testCase.field, backendErr)
            }
        })
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

func TestEveryOperation_ReportsAClosedClientInsteadOfAnsweringOverIt(t *testing.T) {
    backend, client := liveBackend(t)

    client.Close()

    if _, _, getErr := backend.Get(context.Background(), "key"); nil == getErr {
        t.Fatal("Get over a closed client must fail")
    }

    if setErr := backend.Set(context.Background(), "key", []byte("payload"), bound.Of(time.Minute)); nil == setErr {
        t.Fatal("Set over a closed client must fail")
    }

    if setErr := backend.Set(context.Background(), "key", []byte("payload"), bound.UnlimitedDuration()); nil == setErr {
        t.Fatal("Set without an expiry over a closed client must fail")
    }

    if deleteErr := backend.Delete(context.Background(), "key"); nil == deleteErr {
        t.Fatal("Delete over a closed client must fail")
    }

    if _, hasErr := backend.Has(context.Background(), "key"); nil == hasErr {
        t.Fatal("Has over a closed client must fail")
    }

    if clearErr := backend.Clear(context.Background()); nil == clearErr {
        t.Fatal("Clear over a closed client must fail")
    }

    if clearErr := backend.ClearByPrefix(context.Background(), "session:"); nil == clearErr {
        t.Fatal("ClearByPrefix over a closed client must fail")
    }

    if _, manyErr := backend.Many(context.Background(), []string{"key"}); nil == manyErr {
        t.Fatal("Many over a closed client must fail")
    }

    if setErr := backend.SetMultiple(context.Background(), map[string][]byte{"key": []byte("payload")}, bound.Of(time.Minute)); nil == setErr {
        t.Fatal("SetMultiple over a closed client must fail")
    }

    if deleteErr := backend.DeleteMultiple(context.Background(), []string{"key"}); nil == deleteErr {
        t.Fatal("DeleteMultiple over a closed client must fail")
    }

    if _, incrementErr := backend.Increment(context.Background(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == incrementErr {
        t.Fatal("Increment over a closed client must fail")
    }

    if _, decrementErr := backend.Decrement(context.Background(), "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == decrementErr {
        t.Fatal("Decrement over a closed client must fail")
    }
}

func TestDeleteMultiple_AFailureNamesTheKeyAndTheBatchSize(t *testing.T) {
    backend, client := liveBackend(t)

    client.Close()

    deleteErr := backend.DeleteMultiple(context.Background(), []string{"zulu", "alpha"})
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

func TestSetMultiple_AFailureNamesOneOfTheKeys(t *testing.T) {
    backend, client := liveBackend(t)

    client.Close()

    setErr := backend.SetMultiple(context.Background(), map[string][]byte{"only": []byte("payload")}, bound.Of(time.Minute))
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

func TestClose_RefusesLaterOperationsAndLeavesTheSharedClientOpen(t *testing.T) {
    backend, client := liveBackend(t)

    if closeErr := backend.CloseWithContext(t.Context()); nil != closeErr {
        t.Fatalf("Close: %v", closeErr)
    }

    setErr := backend.Set(context.Background(), "after-close", []byte("payload"), bound.Of(time.Minute))
    if nil == setErr {
        t.Fatal("expected the closed backend to refuse")
    }

    if false == strings.Contains(setErr.Error(), "cache backend is closed") {
        t.Fatalf("expected the in-memory backend's refusal, got %v", setErr)
    }

    sibling, siblingErr := NewBackend(client, BackendConfig{Prefix: "melody:test:" + t.Name() + ":sibling:"})
    if nil != siblingErr {
        t.Fatalf("sibling backend: %v", siblingErr)
    }
    t.Cleanup(func() {
        _ = sibling.Clear(context.Background())
    })

    if siblingSetErr := sibling.Set(context.Background(), "after-close", []byte("payload"), bound.Of(time.Minute)); nil != siblingSetErr {
        t.Fatalf("the shared client must stay open for its other borrowers, got %v", siblingSetErr)
    }
}

func TestClearByPrefix_RefusesTheEmptyPrefix(t *testing.T) {
    backend, _ := liveBackend(t)

    ctx := context.Background()

    if setErr := backend.Set(ctx, "tenant:1:a", []byte("x"), bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("set: %v", setErr)
    }
    if setErr := backend.Set(ctx, "other:b", []byte("y"), bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("set: %v", setErr)
    }

    clearErr := backend.ClearByPrefix(ctx, "")
    if nil == clearErr {
        t.Fatal("expected the empty prefix to be refused")
    }

    if false == strings.Contains(clearErr.Error(), "cache key is empty") {
        t.Fatalf("expected the empty-key refusal, got %v", clearErr)
    }

    hasTenant, _ := backend.Has(ctx, "tenant:1:a")
    hasOther, _ := backend.Has(ctx, "other:b")
    if false == hasTenant || false == hasOther {
        t.Fatalf("expected the namespace to survive the refused wipe (tenant=%v other=%v)", hasTenant, hasOther)
    }
}

func TestSetMultiple_RefusesANegativeTtlEvenForAnEmptyBatch(t *testing.T) {
    backend, _ := liveBackend(t)

    setErr := backend.SetMultiple(context.Background(), map[string][]byte{}, bound.Of(-time.Second))
    if nil == setErr {
        t.Fatal("expected the negative ttl to be refused for the empty batch too")
    }

    if "ttl" != exception.LogContext(setErr, exception.LogContextConfig{})["field"] {
        t.Fatalf("expected the negative ttl refusal, got %v", setErr)
    }
}

func TestIncrement_WrapsTheStoreRefusalWithTheKey(t *testing.T) {
    backend, _ := liveBackend(t)

    ctx := context.Background()

    if setErr := backend.Set(ctx, "counter", []byte("not-a-number"), bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("set: %v", setErr)
    }

    _, incrementErr := backend.Increment(ctx, "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
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

func TestCommandTimeout_BoundsTheContractCalls(t *testing.T) {
    _, client := liveBackend(t)

    bounded, boundedErr := NewBackend(client, BackendConfig{Prefix: "melody:test:" + t.Name() + ":"})
    if nil != boundedErr {
        t.Fatalf("backend: %v", boundedErr)
    }

    bounded.commandTimeout = time.Nanosecond

    _, _, getErr := bounded.Get(context.Background(), "any")
    if nil == getErr {
        t.Fatal("expected the one-nanosecond bound to cut the call")
    }

    if false == errors.Is(getErr, context.DeadlineExceeded) || false == errors.Is(getErr, ErrCommandTimeout) {
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

func TestSetMultiple_NamesTheSortedFirstMalformedKey(t *testing.T) {
    backend, _ := liveBackend(t)

    items := map[string][]byte{
        "zz bad": []byte("z"),
        "aa bad": []byte("a"),
        "good":   []byte("g"),
    }

    for attempt := 0; attempt < 8; attempt++ {
        setErr := backend.SetMultiple(context.Background(), items, bound.UnlimitedDuration())
        if nil == setErr {
            t.Fatal("expected the malformed keys to be refused")
        }

        namedKey := exception.LogContext(setErr, exception.LogContextConfig{})["key"]
        if "aa bad" != namedKey {
            t.Fatalf("expected the sorted-first malformed key named, got %v", namedKey)
        }
    }
}

func TestDeleteKeysInBatches_AMultiBatchFailureReportsTheOperationsExtent(t *testing.T) {
    backend, _ := liveBackend(t)

    keyCount := backend.deleteBatch + 10
    keys := make([]string, 0, keyCount)
    for index := 0; index < keyCount; index++ {
        keys = append(keys, fmt.Sprintf("%sextent-%05d", backend.prefix, index))
    }

    cancelledContext, cancel := context.WithCancel(context.Background())
    cancel()

    deleteErr := backend.deleteKeysInBatches(backend.walkRoundTrip(cancelledContext, "Clear"), keys)
    if nil == deleteErr {
        t.Fatal("expected the cancelled batch walk to fail")
    }

    extent := exception.LogContext(deleteErr, exception.LogContextConfig{})
    if nil == extent["deletedBeforeFailure"] || nil == extent["requestedTotal"] {
        t.Fatalf("expected the operation-level extent on the error, got %v", extent)
    }

    singleBatchErr := backend.deleteKeysInBatches(backend.walkRoundTrip(cancelledContext, "Clear"), keys[:1])
    if nil == singleBatchErr {
        t.Fatal("expected the cancelled single batch to fail")
    }

    if nil != exception.LogContext(singleBatchErr, exception.LogContextConfig{})["deletedBeforeFailure"] {
        t.Fatal("expected the single-batch operation to keep the batch report as its own")
    }
}

func TestBackend_CounterRefusalsAreNamedTheWayTheInMemorySiblingNamesThem(t *testing.T) {
    for _, testCase := range []struct {
        name    string
        cause   error
        message string
    }{
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

func TestBackend_CounterRefusalsAreMatchedInsideTheServersOwnWrapping(t *testing.T) {
    refusal := counterError(
        "probe-key",
        errors.New("ERR Error running script (call to f_abc): @user_script:4: VALUE IS NOT AN INTEGER OR OUT OF RANGE"),
    )

    if "cache value is not a valid int64" != refusal.Error() {
        t.Fatalf("message = %q, want the payload refusal", refusal.Error())
    }
}

func TestNewBackend_DefaultMaxKeyLength(t *testing.T) {
    client := &closeTrackingClient{}

    backend, backendErr := NewBackend(client, BackendConfig{})
    if nil != backendErr {
        t.Fatalf("NewBackend returned an error: %v", backendErr)
    }

    if 1024 != backend.maxKeyLength {
        t.Fatalf("expected default max key length 1024, got %d", backend.maxKeyLength)
    }
}

func TestNewBackend_MaxKeyLengthOverrideRejectsLongKey(t *testing.T) {
    client := &closeTrackingClient{}

    backend, backendErr := NewBackend(client, BackendConfig{MaxKeyLength: bound.Of(8)})
    if nil != backendErr {
        t.Fatalf("NewBackend returned an error: %v", backendErr)
    }

    if 8 != backend.maxKeyLength {
        t.Fatalf("expected max key length 8, got %d", backend.maxKeyLength)
    }

    if _, normalizeErr := backend.normalizeKey("123456789"); nil == normalizeErr {
        t.Fatalf("expected a key longer than the configured maximum to be rejected")
    }

    if _, normalizeErr := backend.normalizeKey("12345678"); nil != normalizeErr {
        t.Fatalf("expected a key at the configured maximum to be accepted, got %v", normalizeErr)
    }
}

func TestNormalizeKey_ARefusalNamesAnOversizedKeyByABoundedPrefix(t *testing.T) {
    marker := "...(truncated)"
    backend := &Backend{maxKeyLength: backendMaxKeyLengthRule.Default}

    for name, key := range map[string]string{
        "too long":       strings.Repeat("k", 1<<20),
        "with a space":   strings.Repeat("k", 1<<20) + " ",
        "with a newline": strings.Repeat("k", 1<<20) + "\n",
    } {
        _, refusal := backend.normalizeKey(key)

        var refusalException *exception.Error
        if false == errors.As(refusal, &refusalException) {
            t.Fatalf("%s: expected an exception, got %v", name, refusal)
        }

        rendered, _ := refusalException.Context()["key"].(string)
        if len(rendered) > 128+len(marker) || false == strings.HasSuffix(rendered, marker) {
            t.Fatalf("%s: expected the key cut to 128 bytes with the marker, got %d bytes", name, len(rendered))
        }
    }
}

func TestBackend_CloseRefusesEveryLaterDoor(t *testing.T) {
    backend := &Backend{}
    backend.closed.Store(true)
    ctx := context.Background()

    for door, call := range map[string]func() error{
        "Get":            func() error { _, _, err := backend.Get(ctx, "key"); return err },
        "Set":            func() error { return backend.Set(ctx, "key", []byte("value"), bound.Of(time.Minute)) },
        "Delete":         func() error { return backend.Delete(ctx, "key") },
        "Has":            func() error { _, err := backend.Has(ctx, "key"); return err },
        "Clear":          func() error { return backend.Clear(ctx) },
        "ClearByPrefix":  func() error { return backend.ClearByPrefix(ctx, "prefix") },
        "Many":           func() error { _, err := backend.Many(ctx, []string{"key"}); return err },
        "SetMultiple":    func() error { return backend.SetMultiple(ctx, map[string][]byte{"key": []byte("value")}, bound.Of(time.Minute)) },
        "DeleteMultiple": func() error { return backend.DeleteMultiple(ctx, []string{"key"}) },
        "Increment":      func() error { _, err := backend.Increment(ctx, "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); return err },
        "Decrement":      func() error { _, err := backend.Decrement(ctx, "counter", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); return err },
    } {
        if closedErr := call(); nil == closedErr || false == strings.Contains(closedErr.Error(), "cache backend is closed") {
            t.Fatalf("expected %s refused after Close, got %v", door, closedErr)
        }
    }
}

type delayedClient struct {
    rueidis.Client
    delay time.Duration
}

func (instance *delayedClient) wait(ctx context.Context) {
    select {
    case <-time.After(instance.delay):
    case <-ctx.Done():
    }
}

func (instance *delayedClient) Do(ctx context.Context, command rueidis.Completed) rueidis.RedisResult {
    instance.wait(ctx)

    return instance.Client.Do(ctx, command)
}

func (instance *delayedClient) DoMulti(ctx context.Context, commands ...rueidis.Completed) []rueidis.RedisResult {
    instance.wait(ctx)

    return instance.Client.DoMulti(ctx, commands...)
}

func (instance *delayedClient) Nodes() map[string]rueidis.Client {
    nodes := instance.Client.Nodes()

    delayed := make(map[string]rueidis.Client, len(nodes))
    for name, node := range nodes {
        delayed[name] = &delayedClient{Client: node, delay: instance.delay}
    }

    return delayed
}

func newDelayedNamespaceBackend(t *testing.T, delay time.Duration, commandTimeout time.Duration) (*Backend, []string) {
    t.Helper()

    _, client := liveBackend(t)

    prefix := "melody:test:wide:" + t.Name() + ":"

    filler, fillerErr := NewBackend(client, BackendConfig{Prefix: prefix})
    if nil != fillerErr {
        t.Fatalf("NewBackend: %v", fillerErr)
    }

    keys := make([]string, 0, 30)
    items := make(map[string][]byte, 30)
    for index := 0; index < 30; index = index + 1 {
        key := "key-" + strconv.Itoa(index)
        keys = append(keys, key)
        items[key] = []byte("payload")
    }

    if setErr := filler.SetMultiple(context.Background(), items, bound.Of(time.Minute)); nil != setErr {
        t.Fatalf("SetMultiple: %v", setErr)
    }

    t.Cleanup(func() {
        if clearErr := filler.Clear(context.Background()); nil != clearErr {
            t.Errorf("cleanup: %v", clearErr)
        }
    })

    backend, backendErr := NewBackend(&delayedClient{Client: client, delay: delay}, BackendConfig{
        Prefix:         prefix,
        ScanCount:      bound.Of(1),
        DeleteBatch:    bound.Of(1),
        CommandTimeout: bound.Of(commandTimeout),
    })
    if nil != backendErr {
        t.Fatalf("NewBackend: %v", backendErr)
    }

    return backend, keys
}

func TestClear_BoundsEachRoundTripNotTheWalk(t *testing.T) {
    backend, keys := newDelayedNamespaceBackend(t, 20*time.Millisecond, 500*time.Millisecond)

    if clearErr := backend.Clear(context.Background()); nil != clearErr {
        t.Fatalf("expected a namespace wider than one command timeout cleared, got %v", clearErr)
    }

    values, manyErr := backend.Many(context.Background(), keys)
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 0 != len(values) {
        t.Fatalf("expected the namespace emptied, %d survived", len(values))
    }
}

func TestClearByPrefix_BoundsEachRoundTripNotTheWalk(t *testing.T) {
    backend, keys := newDelayedNamespaceBackend(t, 20*time.Millisecond, 500*time.Millisecond)

    if clearErr := backend.ClearByPrefix(context.Background(), "key-"); nil != clearErr {
        t.Fatalf("expected a subtree wider than one command timeout cleared, got %v", clearErr)
    }

    values, manyErr := backend.Many(context.Background(), keys)
    if nil != manyErr {
        t.Fatalf("Many: %v", manyErr)
    }

    if 0 != len(values) {
        t.Fatalf("expected the subtree emptied, %d survived", len(values))
    }
}

func TestClear_TheCallersContextBoundsTheWholeWalk(t *testing.T) {
    backend, _ := newDelayedNamespaceBackend(t, 20*time.Millisecond, time.Minute)

    ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    if clearErr := backend.Clear(ctx); false == errors.Is(clearErr, context.DeadlineExceeded) {
        t.Fatalf("expected the caller's deadline to end the walk, got %v", clearErr)
    }
}

func TestClearByPrefix_TheCallersContextBoundsTheWholeWalk(t *testing.T) {
    backend, _ := newDelayedNamespaceBackend(t, 20*time.Millisecond, time.Minute)

    ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
    defer cancel()

    if clearErr := backend.ClearByPrefix(ctx, "key-"); false == errors.Is(clearErr, context.DeadlineExceeded) {
        t.Fatalf("expected the caller's deadline to end the walk, got %v", clearErr)
    }
}

func TestClear_AWedgedRoundTripIsCutByTheCommandTimeout(t *testing.T) {
    backend, _ := newDelayedNamespaceBackend(t, time.Hour, 50*time.Millisecond)

    finished := make(chan error, 1)
    go func() {
        finished <- backend.Clear(context.Background())
    }()

    select {
    case clearErr := <-finished:
        if false == errors.Is(clearErr, context.DeadlineExceeded) || false == errors.Is(clearErr, ErrCommandTimeout) {
            t.Fatalf("expected the command timeout to cut the round trip, got %v", clearErr)
        }
    case <-time.After(10 * time.Second):
        t.Fatalf("expected a wedged round trip cut at the command timeout")
    }
}

func backendContextOperations(backend *Backend) map[string]func(ctx context.Context, key string) error {
    return map[string]func(ctx context.Context, key string) error{
        "Get": func(ctx context.Context, key string) error {
            _, _, err := backend.Get(ctx, key)
            return err
        },
        "Set": func(ctx context.Context, key string) error {
            return backend.Set(ctx, key, []byte("written"), bound.Of(time.Minute))
        },
        "Delete": func(ctx context.Context, key string) error {
            return backend.Delete(ctx, key)
        },
        "Has": func(ctx context.Context, key string) error {
            _, err := backend.Has(ctx, key)
            return err
        },
        "Clear": func(ctx context.Context, key string) error {
            return backend.Clear(ctx)
        },
        "ClearByPrefix": func(ctx context.Context, key string) error {
            return backend.ClearByPrefix(ctx, key)
        },
        "Many": func(ctx context.Context, key string) error {
            _, err := backend.Many(ctx, []string{key})
            return err
        },
        "SetMultiple": func(ctx context.Context, key string) error {
            return backend.SetMultiple(ctx, map[string][]byte{key: []byte("written")}, bound.Of(time.Minute))
        },
        "DeleteMultiple": func(ctx context.Context, key string) error {
            return backend.DeleteMultiple(ctx, []string{key})
        },
        "Increment": func(ctx context.Context, key string) error {
            _, err := backend.Increment(ctx, key, 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
            return err
        },
        "Decrement": func(ctx context.Context, key string) error {
            _, err := backend.Decrement(ctx, key, 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
            return err
        },
    }
}

type typedNilContext struct {
    context.Context
}

func TestBackend_EveryDoorRefusesANilContextFirst(t *testing.T) {
    backend, backendErr := NewBackend(&closeTrackingClient{}, BackendConfig{})
    if nil != backendErr {
        t.Fatalf("NewBackend: %v", backendErr)
    }

    if closeErr := backend.CloseWithContext(context.Background()); nil != closeErr {
        t.Fatalf("close: %v", closeErr)
    }

    var absentContext *typedNilContext

    for name, operation := range backendContextOperations(backend) {
        for _, operationContext := range []context.Context{nil, absentContext} {
            operationErr := operation(operationContext, "")
            if nil == operationErr || "cache context is nil" != operationErr.Error() {
                t.Fatalf("%s: expected the absent context refused first, got %v", name, operationErr)
            }
        }
    }
}

func TestBackend_ADoneContextIsAnsweredAfterTheArgumentsAndTouchesNothing(t *testing.T) {
    backend, _ := liveBackend(t)

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    for name, operation := range backendContextOperations(backend) {
        if setErr := backend.Set(context.Background(), "held", []byte("7"), bound.Of(time.Minute)); nil != setErr {
            t.Fatalf("planting: %v", setErr)
        }

        if "Clear" != name {
            if keyErr := operation(canceledContext, ""); nil == keyErr || true == errors.Is(keyErr, context.Canceled) {
                t.Fatalf("%s: expected the malformed key refused before the done context, got %v", name, keyErr)
            }
        }

        for _, key := range []string{"held", "fresh"} {
            if doneErr := operation(canceledContext, key); false == errors.Is(doneErr, context.Canceled) {
                t.Fatalf("%s %s: expected the done context's error, got %v", name, key, doneErr)
            }
        }

        payload, exists, getErr := backend.Get(context.Background(), "held")
        if nil != getErr || false == exists || "7" != string(payload) {
            t.Fatalf("%s: expected the held entry untouched, got %q, %v, %v", name, payload, exists, getErr)
        }

        if freshExists, hasErr := backend.Has(context.Background(), "fresh"); nil != hasErr || true == freshExists {
            t.Fatalf("%s: expected nothing written under the done context, got %v, %v", name, freshExists, hasErr)
        }
    }
}

func TestBackend_ARoundTripCutByTheCommandTimeoutNamesTheField(t *testing.T) {
    _, client := liveBackend(t)

    backend, backendErr := NewBackend(&delayedClient{Client: client, delay: time.Hour}, BackendConfig{
        Prefix:         "melody:test:" + t.Name() + ":",
        CommandTimeout: bound.Of(50 * time.Millisecond),
    })
    if nil != backendErr {
        t.Fatalf("NewBackend: %v", backendErr)
    }

    for name, operation := range backendContextOperations(backend) {
        finished := make(chan error, 1)
        go func() {
            finished <- operation(context.Background(), "wedged")
        }()

        select {
        case operationErr := <-finished:
            if false == errors.Is(operationErr, context.DeadlineExceeded) || false == errors.Is(operationErr, ErrCommandTimeout) {
                t.Fatalf("%s: expected the command timeout to cut the round trip, got %v", name, operationErr)
            }

            var melodyErr *exception.Error
            if false == errors.As(operationErr, &melodyErr) {
                t.Fatalf("%s: expected a melody error, got %T", name, operationErr)
            }

            namedErr := melodyErr
            for nil != namedErr && "BackendConfig.CommandTimeout" != namedErr.Context()["field"] {
                var nextErr *exception.Error
                if false == errors.As(namedErr.Unwrap(), &nextErr) {
                    nextErr = nil
                }
                namedErr = nextErr
            }

            if nil == namedErr || "50ms" != namedErr.Context()["commandTimeout"] || name != namedErr.Context()["operation"] {
                t.Fatalf("%s: expected the error to name the field, the timeout and the door, got %v", name, operationErr)
            }
        case <-time.After(10 * time.Second):
            t.Fatalf("%s: expected a wedged round trip cut at the command timeout", name)
        }
    }
}

type deleteDelayedClient struct {
    rueidis.Client
}

func (instance *deleteDelayedClient) DoMulti(ctx context.Context, commands ...rueidis.Completed) []rueidis.RedisResult {
    <-ctx.Done()

    return instance.Client.DoMulti(ctx, commands...)
}

func TestClear_ADeleteRoundTripCutByTheCommandTimeoutNamesTheField(t *testing.T) {
    _, client := liveBackend(t)

    for _, operation := range []string{"Clear", "ClearByPrefix"} {
        prefix := "melody:test:" + t.Name() + ":" + operation + ":"

        filler, fillerErr := NewBackend(client, BackendConfig{Prefix: prefix})
        if nil != fillerErr {
            t.Fatalf("NewBackend: %v", fillerErr)
        }
        if setErr := filler.Set(context.Background(), "key-held", []byte("payload"), bound.Of(time.Minute)); nil != setErr {
            t.Fatalf("Set: %v", setErr)
        }
        t.Cleanup(func() { _ = filler.Clear(context.Background()) })

        backend, backendErr := NewBackend(&deleteDelayedClient{Client: client}, BackendConfig{Prefix: prefix, CommandTimeout: bound.Of(50 * time.Millisecond)})
        if nil != backendErr {
            t.Fatalf("NewBackend: %v", backendErr)
        }

        var clearErr error
        if "Clear" == operation {
            clearErr = backend.Clear(context.Background())
        } else {
            clearErr = backend.ClearByPrefix(context.Background(), "key-")
        }

        if false == errors.Is(clearErr, ErrCommandTimeout) {
            t.Fatalf("%s: expected the delete round trip cut by the command timeout named, got %v", operation, clearErr)
        }
    }
}

func runWedgedOperations(t *testing.T, commandTimeout time.Duration, callerContext func() (context.Context, context.CancelFunc), check func(name string, operationErr error)) {
    t.Helper()

    _, client := liveBackend(t)

    backend, backendErr := NewBackend(&delayedClient{Client: client, delay: time.Hour}, BackendConfig{
        Prefix:         "melody:test:" + t.Name() + ":",
        CommandTimeout: bound.Of(commandTimeout),
    })
    if nil != backendErr {
        t.Fatalf("NewBackend: %v", backendErr)
    }

    for name, operation := range backendContextOperations(backend) {
        ctx, cancel := callerContext()
        finished := make(chan error, 1)
        go func() {
            finished <- operation(ctx, "wedged")
        }()

        select {
        case operationErr := <-finished:
            check(name, operationErr)
        case <-time.After(10 * time.Second):
            t.Fatalf("%s: expected a wedged round trip cut by the caller", name)
        }
        cancel()
    }
}

func TestBackend_ACallersOwnDeadlineIsNotNamedTheCommandTimeout(t *testing.T) {
    runWedgedOperations(t, time.Second, func() (context.Context, context.CancelFunc) {
        return context.WithTimeout(context.Background(), 20*time.Millisecond)
    }, func(name string, operationErr error) {
        if false == errors.Is(operationErr, context.DeadlineExceeded) || true == errors.Is(operationErr, ErrCommandTimeout) {
            t.Fatalf("%s: expected the caller's deadline answered without the command timeout's name, got %v", name, operationErr)
        }
    })
}

func TestBackend_ACallersCancellationIsNotNamedTheCommandTimeout(t *testing.T) {
    runWedgedOperations(t, time.Second, func() (context.Context, context.CancelFunc) {
        ctx, cancel := context.WithCancel(context.Background())
        time.AfterFunc(20*time.Millisecond, cancel)

        return ctx, cancel
    }, func(name string, operationErr error) {
        if false == errors.Is(operationErr, context.Canceled) || true == errors.Is(operationErr, ErrCommandTimeout) {
            t.Fatalf("%s: expected the caller's cancellation answered without the command timeout's name, got %v", name, operationErr)
        }
    })
}

func TestBackend_ACounterCutByTheCommandTimeoutIsNotAnsweredAsAStoreFailure(t *testing.T) {
    runWedgedOperations(t, 50*time.Millisecond, func() (context.Context, context.CancelFunc) {
        return context.WithCancel(context.Background())
    }, func(name string, operationErr error) {
        if "Increment" != name && "Decrement" != name {
            return
        }

        if nil == operationErr || "cache round trip was cut by BackendConfig.CommandTimeout" != operationErr.Error() || true == strings.Contains(operationErr.Error(), counterStoreFailureMessage) {
            t.Fatalf("%s: expected the command timeout's message, got %v", name, operationErr)
        }
    })
}

func TestRoundTripError_AnsweredOnlyForADeadlineTheCommandTimeoutCaused(t *testing.T) {
    backend := &Backend{commandTimeout: 50 * time.Millisecond}

    commandTimedOut, cancelCommandTimedOut := context.WithTimeoutCause(context.Background(), time.Nanosecond, ErrCommandTimeout)
    defer cancelCommandTimedOut()
    <-commandTimedOut.Done()

    callerContext, cancelCaller := context.WithTimeout(context.Background(), time.Nanosecond)
    defer cancelCaller()
    <-callerContext.Done()
    callerTimedOut, cancelCallerTimedOut := context.WithTimeoutCause(callerContext, time.Hour, ErrCommandTimeout)
    defer cancelCallerTimedOut()

    if nil != backend.roundTripError(commandTimedOut, "Get", nil) {
        t.Fatal("expected no error answered as none")
    }

    redisErr := errors.New("ERR wrong number of arguments")
    if redisErr != backend.roundTripError(commandTimedOut, "Get", redisErr) {
        t.Fatal("expected an error that is no deadline answered as it came")
    }

    if namedErr := backend.roundTripError(callerTimedOut, "Get", context.DeadlineExceeded); context.DeadlineExceeded != namedErr {
        t.Fatalf("expected the caller's deadline answered as it came, got %v", namedErr)
    }

    namedErr := backend.roundTripError(commandTimedOut, "Get", context.DeadlineExceeded)
    if false == errors.Is(namedErr, ErrCommandTimeout) || false == errors.Is(namedErr, context.DeadlineExceeded) || false == exception.ChainIs(namedErr, ErrCommandTimeout) {
        t.Fatalf("expected the command timeout named, got %v", namedErr)
    }
}

type earlyDeadlineContext struct {
    context.Context
}

func (instance earlyDeadlineContext) Deadline() (time.Time, bool) {
    return time.Now().Add(-time.Millisecond), true
}

func TestRoundTripError_ADeadlineTheClientReadBeforeTheTimerFiredIsStillNamed(t *testing.T) {
    backend := &Backend{commandTimeout: 30 * time.Millisecond}

    commandContext, cancel := context.WithTimeoutCause(context.Background(), 30*time.Millisecond, ErrCommandTimeout)
    defer cancel()

    namedErr := backend.roundTripError(earlyDeadlineContext{Context: commandContext}, "Get", context.DeadlineExceeded)
    if false == errors.Is(namedErr, ErrCommandTimeout) {
        t.Fatalf("expected a deadline the client answered before the timer fired named, got %v", namedErr)
    }

    callerContext, cancelCaller := context.WithTimeout(context.Background(), 30*time.Millisecond)
    defer cancelCaller()
    callerBound, cancelCallerBound := context.WithTimeoutCause(callerContext, time.Hour, ErrCommandTimeout)
    defer cancelCallerBound()

    if callerErr := backend.roundTripError(earlyDeadlineContext{Context: callerBound}, "Get", context.DeadlineExceeded); true == errors.Is(callerErr, ErrCommandTimeout) {
        t.Fatalf("expected the caller's deadline read early left unnamed, got %v", callerErr)
    }
}

func TestRoundTripError_ADeadlineThatNeverSignalsIsAnsweredUnnamedPastTheGrace(t *testing.T) {
    backend := &Backend{commandTimeout: time.Second}

    answered := make(chan error, 1)
    go func() {
        answered <- backend.roundTripError(earlyDeadlineContext{Context: context.Background()}, "Get", context.DeadlineExceeded)
    }()

    select {
    case answeredErr := <-answered:
        if context.DeadlineExceeded != answeredErr {
            t.Fatalf("expected the round trip's error answered as it came, got %v", answeredErr)
        }
    case <-time.After(2 * time.Second):
        t.Fatal("expected a deadline that never signals answered past the grace, the naming still waits")
    }
}

func TestErrCommandTimeout_UnwrapsToTheDeadlineAndIsATimeout(t *testing.T) {
    if false == errors.Is(ErrCommandTimeout, context.DeadlineExceeded) {
        t.Fatal("expected the sentinel to unwrap to the deadline")
    }

    timeoutError, isTimeoutError := ErrCommandTimeout.(interface{ Timeout() bool })
    if false == isTimeoutError || false == timeoutError.Timeout() {
        t.Fatal("expected the sentinel to be a timeout")
    }
}

func TestBackend_AnEmptyBatchUnderADoneContextAnswersItsError(t *testing.T) {
    backend, backendErr := NewBackend(&closeTrackingClient{}, BackendConfig{})
    if nil != backendErr {
        t.Fatalf("NewBackend: %v", backendErr)
    }

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if _, manyErr := backend.Many(canceledContext, nil); false == errors.Is(manyErr, context.Canceled) {
        t.Fatalf("Many: expected the done context's error, got %v", manyErr)
    }

    if setErr := backend.SetMultiple(canceledContext, nil, bound.Of(time.Minute)); false == errors.Is(setErr, context.Canceled) {
        t.Fatalf("SetMultiple: expected the done context's error, got %v", setErr)
    }

    if deleteErr := backend.DeleteMultiple(canceledContext, nil); false == errors.Is(deleteErr, context.Canceled) {
        t.Fatalf("DeleteMultiple: expected the done context's error, got %v", deleteErr)
    }
}

type commandCountingClient struct {
    rueidis.Client
    commands *atomic.Int64
}

func (instance *commandCountingClient) Nodes() map[string]rueidis.Client {
    nodes := instance.Client.Nodes()

    counting := make(map[string]rueidis.Client, len(nodes))
    for name, node := range nodes {
        counting[name] = &commandCountingClient{Client: node, commands: instance.commands}
    }

    return counting
}

func (instance *commandCountingClient) Do(ctx context.Context, command rueidis.Completed) rueidis.RedisResult {
    instance.commands.Add(1)

    return instance.Client.Do(ctx, command)
}

func (instance *commandCountingClient) DoMulti(ctx context.Context, commands ...rueidis.Completed) []rueidis.RedisResult {
    instance.commands.Add(1)

    return instance.Client.DoMulti(ctx, commands...)
}

func TestBackend_ADoneContextSendsNoCommandToTheClient(t *testing.T) {
    _, client := liveBackend(t)

    countingClient := &commandCountingClient{Client: client, commands: &atomic.Int64{}}
    backend, backendErr := NewBackend(countingClient, BackendConfig{Prefix: "melody:test:" + t.Name() + ":"})
    if nil != backendErr {
        t.Fatalf("NewBackend: %v", backendErr)
    }

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    for name, operation := range backendContextOperations(backend) {
        if doneErr := operation(canceledContext, "key"); false == errors.Is(doneErr, context.Canceled) {
            t.Fatalf("%s: expected the done context's error, got %v", name, doneErr)
        }

        if 0 != countingClient.commands.Load() {
            t.Fatalf("%s: expected no command handed to the client under a done context, got %d", name, countingClient.commands.Load())
        }
    }
}

func TestBackend_UnderADoneContextTheTtlAndTheDeltaAreJudgedFirst(t *testing.T) {
    backend, backendErr := NewBackend(&closeTrackingClient{}, BackendConfig{})
    if nil != backendErr {
        t.Fatalf("NewBackend: %v", backendErr)
    }

    canceledContext, cancel := context.WithCancel(context.Background())
    cancel()

    if setErr := backend.Set(canceledContext, "key", []byte("value"), bound.Duration{}); nil == setErr || true == errors.Is(setErr, context.Canceled) {
        t.Fatalf("Set: expected the absent ttl refused before the done context, got %v", setErr)
    }

    if setErr := backend.SetMultiple(canceledContext, map[string][]byte{"key": []byte("value")}, bound.Duration{}); nil == setErr || true == errors.Is(setErr, context.Canceled) {
        t.Fatalf("SetMultiple: expected the absent ttl refused before the done context, got %v", setErr)
    }

    _, decrementErr := backend.Decrement(canceledContext, "key", math.MinInt64, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()})
    if nil == decrementErr || "delta overflows int64 when negated" != decrementErr.Error() {
        t.Fatalf("Decrement: expected the in-memory backend's delta refusal before the done context, got %v", decrementErr)
    }
}

func TestBackend_ACounterWritesItsTtlAsItsWindowSays(t *testing.T) {
    backend, client := liveBackend(t)
    ctx := context.Background()
    prefix := "melody:test:" + t.Name() + ":"

    fixed := cachecontract.CounterOptions{Ttl: bound.Of(time.Hour)}
    backend.Increment(ctx, "fixed", 1, fixed)
    if armed, armErr := client.Do(ctx, client.B().Pexpire().Key(prefix+"fixed").Milliseconds(600000).Build()).AsInt64(); nil != armErr || 1 != armed {
        t.Fatalf("could not arm the fixed window: %d %v", armed, armErr)
    }
    if value, _ := backend.Increment(ctx, "fixed", 1, fixed); 2 != value {
        t.Fatalf("expected 2, got %d", value)
    }
    if remaining := rawTtl(t, client, prefix+"fixed"); 0 >= remaining || remaining > 600000 {
        t.Fatalf("expected the fixed window left in place, got %d ms", remaining)
    }

    sliding := cachecontract.CounterOptions{Ttl: bound.Of(time.Hour), RefreshTtl: true}
    backend.Increment(ctx, "sliding", 1, sliding)
    if armed, armErr := client.Do(ctx, client.B().Pexpire().Key(prefix+"sliding").Milliseconds(600000).Build()).AsInt64(); nil != armErr || 1 != armed {
        t.Fatalf("could not arm the sliding window: %d %v", armed, armErr)
    }
    backend.Decrement(ctx, "sliding", 1, sliding)
    if remaining := rawTtl(t, client, prefix+"sliding"); remaining < 3500000 {
        t.Fatalf("expected the sliding window rewritten to an hour, got %d ms", remaining)
    }

    backend.Set(ctx, "bare", []byte("7"), bound.UnlimitedDuration())
    if value, _ := backend.Increment(ctx, "bare", 1, fixed); 8 != value {
        t.Fatalf("expected 8, got %d", value)
    }
    if remaining := rawTtl(t, client, prefix+"bare"); remaining < 3500000 {
        t.Fatalf("expected the fixed window written on a key without an expiry, got %d ms", remaining)
    }

    backend.Increment(ctx, "forever", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration(), RefreshTtl: true})
    if remaining := rawTtl(t, client, prefix+"forever"); -1 != remaining {
        t.Fatalf("expected an unlimited counter without expiry, got %d", remaining)
    }

    if _, ttlErr := backend.Increment(ctx, "refused", 1, cachecontract.CounterOptions{}); nil == ttlErr || false == strings.Contains(ttlErr.Error(), "ttl is required") {
        t.Fatalf("expected the absent ttl refused, got %v", ttlErr)
    }
}

func TestBackend_AnUnlimitedSlidingCounterPersistsTheKey(t *testing.T) {
    backend, client := liveBackend(t)
    ctx := context.Background()
    prefix := "melody:test:" + t.Name() + ":"

    for _, door := range []string{"Increment", "Decrement"} {
        key := "sliding-" + door
        backend.Increment(ctx, key, 4, cachecontract.CounterOptions{Ttl: bound.Of(time.Hour)})
        if armed, armErr := client.Do(ctx, client.B().Pexpire().Key(prefix+key).Milliseconds(600000).Build()).AsInt64(); nil != armErr || 1 != armed {
            t.Fatalf("%s: could not arm the expiry: %d %v", door, armed, armErr)
        }

        unlimitedSliding := cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration(), RefreshTtl: true}
        var callErr error
        if "Increment" == door {
            _, callErr = backend.Increment(ctx, key, 1, unlimitedSliding)
        } else {
            _, callErr = backend.Decrement(ctx, key, 1, unlimitedSliding)
        }
        if nil != callErr {
            t.Fatalf("%s: %v", door, callErr)
        }

        if remaining := rawTtl(t, client, prefix+key); -1 != remaining {
            t.Fatalf("%s: expected the unlimited sliding counter persisted, got %d", door, remaining)
        }
    }
}

func TestBackend_AnUnlimitedFixedCounterKeepsTheKeysExpiry(t *testing.T) {
    backend, client := liveBackend(t)
    ctx := context.Background()
    prefix := "melody:test:" + t.Name() + ":"

    backend.Increment(ctx, "fixed", 4, cachecontract.CounterOptions{Ttl: bound.Of(time.Hour)})
    if armed, armErr := client.Do(ctx, client.B().Pexpire().Key(prefix+"fixed").Milliseconds(600000).Build()).AsInt64(); nil != armErr || 1 != armed {
        t.Fatalf("could not arm the expiry: %d %v", armed, armErr)
    }

    if value, _ := backend.Increment(ctx, "fixed", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); 5 != value {
        t.Fatalf("expected 5, got %d", value)
    }

    if remaining := rawTtl(t, client, prefix+"fixed"); 0 >= remaining || remaining > 600000 {
        t.Fatalf("expected the unlimited fixed counter to keep the expiry, got %d ms", remaining)
    }
}

func TestBackend_ConcurrentIncrementsWithATtlStayAtomic(t *testing.T) {
    backend, client := liveBackend(t)
    prefix := "melody:test:" + t.Name() + ":"

    var group sync.WaitGroup
    for worker := 0; worker < 20; worker++ {
        group.Add(1)
        go func() {
            defer group.Done()
            for call := 0; call < 25; call++ {
                backend.Increment(context.Background(), "count", 1, cachecontract.CounterOptions{Ttl: bound.Of(time.Hour)})
            }
        }()
    }
    group.Wait()

    if value, _ := backend.Increment(context.Background(), "count", 0, cachecontract.CounterOptions{Ttl: bound.Of(time.Hour)}); 500 != value {
        t.Fatalf("expected 500, got %d", value)
    }
    if remaining := rawTtl(t, client, prefix+"count"); remaining <= 0 {
        t.Fatalf("expected the counter to carry its ttl, got %d", remaining)
    }
}

func TestBackend_ACounterAboveTwoToTheFiftyThirdIsAnsweredExactly(t *testing.T) {
    backend, _ := liveBackend(t)
    ctx := context.Background()

    if setErr := backend.Set(ctx, "large", []byte("9007199254740992"), bound.UnlimitedDuration()); nil != setErr {
        t.Fatal(setErr)
    }

    if value, incrementErr := backend.Increment(ctx, "large", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr || 9007199254740993 != value {
        t.Fatalf("expected 9007199254740993 exactly, got %d %v", value, incrementErr)
    }

    backend.Set(ctx, "edge", []byte("9223372036854775806"), bound.UnlimitedDuration())
    if value, incrementErr := backend.Increment(ctx, "edge", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil != incrementErr || math.MaxInt64 != value {
        t.Fatalf("expected MaxInt64 exactly, got %d %v", value, incrementErr)
    }

    if _, overflowErr := backend.Increment(ctx, "edge", 1, cachecontract.CounterOptions{Ttl: bound.UnlimitedDuration()}); nil == overflowErr || false == strings.Contains(overflowErr.Error(), "cache increment overflow") {
        t.Fatalf("expected the overflow refused, got %v", overflowErr)
    }
}

func TestBackend_ACounterRefusesAnAbsentTtlBeforeACancelledContext(t *testing.T) {
    backend, _ := liveBackend(t)

    cancelled, cancel := context.WithCancel(context.Background())
    cancel()

    if _, ttlErr := backend.Increment(cancelled, "refused", 1, cachecontract.CounterOptions{}); nil == ttlErr || false == strings.Contains(ttlErr.Error(), "ttl is required") {
        t.Fatalf("expected the absent ttl refused before the cancelled context, got %v", ttlErr)
    }
}
