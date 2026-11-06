package output

import (
    "encoding/json"
    "runtime"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
)

func TestNewMeta_HonoursTheCallerGoVersionAndFallsBackToTheRuntime(t *testing.T) {
    withCaller := NewMeta("cmd", nil, Option{}, time.Now(), 0, Version{Go: "go9.9"})
    if "go9.9" != withCaller.Version.Go {
        t.Fatalf("expected the caller's Go version, got %q", withCaller.Version.Go)
    }

    withoutCaller := NewMeta("cmd", nil, Option{}, time.Now(), 0, Version{})
    if runtime.Version() != withoutCaller.Version.Go {
        t.Fatalf("expected the runtime fallback, got %q", withoutCaller.Version.Go)
    }
}

func TestNewMeta_CopiesTheArguments(t *testing.T) {
    arguments := []string{"one"}

    meta := NewMeta("cmd", arguments, Option{}, time.Now(), 0, Version{})

    arguments[0] = "mutated"

    if "one" != meta.Arguments[0] {
        t.Fatalf("expected the copied argument, got %q", meta.Arguments[0])
    }
}

func TestNewMeta_TheJsonFlagsCarryTheLimitAndTheOffsetAsGiven(t *testing.T) {
    for _, testCase := range []struct {
        option   Option
        expected string
    }{
        {Option{}, `"limit":null,"offset":null`},
        {Option{Limit: bound.UnlimitedCount(), Offset: bound.Of(0)}, `"limit":"unlimited","offset":0`},
        {Option{Limit: bound.Of(5), Offset: bound.Of(10)}, `"limit":5,"offset":10`},
    } {
        encoded, marshalErr := json.Marshal(NewMeta("command", nil, testCase.option, time.Now(), 0, Version{}).Flags)
        if nil != marshalErr {
            t.Fatalf("unexpected marshal error: %v", marshalErr)
        }

        if false == strings.Contains(string(encoded), testCase.expected) {
            t.Fatalf("expected %s in %s", testCase.expected, encoded)
        }
    }
}

func TestNewListPayload_TheJsonFormCarriesTheLimitAndTheOffsetAsGiven(t *testing.T) {
    encoded, marshalErr := json.Marshal(NewListPayload([]int{1}, 1, bound.Count{}, bound.Of(3)))
    if nil != marshalErr {
        t.Fatalf("unexpected marshal error: %v", marshalErr)
    }

    if `{"items":[1],"total":1,"limit":null,"offset":3}` != string(encoded) {
        t.Fatalf("unexpected json %s", encoded)
    }
}
