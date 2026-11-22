package output

import (
    "encoding/json"
    "runtime"
    "strings"
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    melodyversion "github.com/precision-soft/melody/v4/version"
)

func TestNewMeta_ZeroConfigFillsTheVersionsAndNormalisesTheOption(t *testing.T) {
    meta := NewMeta("cmd", MetaConfig{})

    if melodyversion.BuildVersion() != meta.Version.Melody || runtime.Version() != meta.Version.Go || "" != meta.Version.Application {
        t.Fatalf("expected the running binary's versions and no application version, got %+v", meta.Version)
    }

    if FormatTable != meta.Flags.Format || SortOrderAscending != meta.Flags.Order {
        t.Fatalf("expected the normalised table format and ascending order, got %+v", meta.Flags)
    }

    if nil == meta.Arguments || 0 != len(meta.Arguments) || 0 != meta.DurationMilliseconds {
        t.Fatalf("expected empty arguments and no duration, got %v and %d", meta.Arguments, meta.DurationMilliseconds)
    }

    optionVersioned := NewMeta("cmd", MetaConfig{Option: Option{ApplicationVersion: "1.2.3"}, Version: Version{Application: "ignored"}})
    if "1.2.3" != optionVersioned.Version.Application {
        t.Fatalf("expected the option's application version, got %q", optionVersioned.Version.Application)
    }
}

func TestNewMeta_HonoursTheCallerGoVersionAndFallsBackToTheRuntime(t *testing.T) {
    withCaller := NewMeta("cmd", MetaConfig{StartedAt: time.Now(), Version: Version{Go: "go9.9"}})
    if "go9.9" != withCaller.Version.Go {
        t.Fatalf("expected the caller's Go version, got %q", withCaller.Version.Go)
    }

    withoutCaller := NewMeta("cmd", MetaConfig{StartedAt: time.Now()})
    if runtime.Version() != withoutCaller.Version.Go {
        t.Fatalf("expected the runtime fallback, got %q", withoutCaller.Version.Go)
    }
}

func TestNewMeta_CopiesTheArguments(t *testing.T) {
    arguments := []string{"one"}

    meta := NewMeta("cmd", MetaConfig{Arguments: arguments, StartedAt: time.Now()})

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
        encoded, marshalErr := json.Marshal(NewMeta("command", MetaConfig{Option: testCase.option, StartedAt: time.Now()}).Flags)
        if nil != marshalErr {
            t.Fatalf("unexpected marshal error: %v", marshalErr)
        }

        if false == strings.Contains(string(encoded), testCase.expected) {
            t.Fatalf("expected %s in %s", testCase.expected, encoded)
        }
    }
}

func TestNewListPayload_TheJsonFormCarriesTheLimitAndTheOffsetAsGiven(t *testing.T) {
    encoded, marshalErr := json.Marshal(NewListPayload([]int{1}, ListPayloadConfig{Total: 1, Offset: bound.Of(3)}))
    if nil != marshalErr {
        t.Fatalf("unexpected marshal error: %v", marshalErr)
    }

    if `{"items":[1],"total":1,"limit":null,"offset":3}` != string(encoded) {
        t.Fatalf("unexpected json %s", encoded)
    }
}
