package output

import (
    "runtime"
    "testing"
    "time"
)

func TestNewMeta_HonoursTheCallerGoVersionAndFallsBackToTheRuntime(t *testing.T) {
    withCaller := NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{Go: "go9.9"})
    if "go9.9" != withCaller.Version.Go {
        t.Fatalf("expected the caller's Go version, got %q", withCaller.Version.Go)
    }

    withoutCaller := NewMeta("cmd", nil, DefaultOption(), time.Now(), 0, Version{})
    if runtime.Version() != withoutCaller.Version.Go {
        t.Fatalf("expected the runtime fallback, got %q", withoutCaller.Version.Go)
    }
}

func TestNewMeta_CopiesTheArguments(t *testing.T) {
    arguments := []string{"one"}

    meta := NewMeta("cmd", arguments, DefaultOption(), time.Now(), 0, Version{})

    arguments[0] = "mutated"

    if "one" != meta.Arguments[0] {
        t.Fatalf("expected the copied argument, got %q", meta.Arguments[0])
    }
}
