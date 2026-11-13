package logging

import (
    "fmt"
    "runtime/debug"
    "testing"
)

/* a cyclic value that reached fmt would recurse until the goroutine stack is gone; the stack is bounded for the
   length of a test, so a regression ends the test binary in milliseconds instead of growing a gigabyte of stack */
func boundTextValueStack() func() {
    previous := debug.SetMaxStack(64 << 20)

    return func() {
        debug.SetMaxStack(previous)
    }
}

type textValueHolder struct {
    Children map[string]any
}

func TestRenderTextValue_RendersAnAcyclicValueAsFmtDoes(t *testing.T) {
    shared := map[string]any{"a": 1}
    value := map[string]any{
        "nested": map[string]any{"b": []any{1, "two", 3.5}},
        "holder": textValueHolder{Children: map[string]any{"c": true}},
        "bytes":  []byte("xyz"),
        "first":  shared,
        "second": shared,
        "error":  fmt.Errorf("refused"),
    }

    if fmt.Sprintf("%v", value) != renderTextValue(value) {
        t.Fatalf("expected an acyclic value rendered exactly as fmt renders it, got %q against %q", renderTextValue(value), fmt.Sprintf("%v", value))
    }
}

func TestRenderTextValue_MarksAMapThatHoldsItself(t *testing.T) {
    defer boundTextValueStack()()

    cyclic := map[string]any{"name": "loop"}
    cyclic["self"] = cyclic

    if "map[name:loop self:<cycle>]" != renderTextValue(cyclic) {
        t.Fatalf("expected the map closing the cycle rendered as the marker, got %q", renderTextValue(cyclic))
    }
}

func TestRenderTextValue_MarksACycleClosedThroughAStructField(t *testing.T) {
    defer boundTextValueStack()()

    cyclic := map[string]any{}
    cyclic["node"] = textValueHolder{Children: cyclic}

    if "map[node:<cycle>]" != renderTextValue(cyclic) {
        t.Fatalf("expected the struct holding the cycle rendered as the marker, got %q", renderTextValue(cyclic))
    }
}

func TestRenderTextValue_MarksASliceThatHoldsItself(t *testing.T) {
    defer boundTextValueStack()()

    cyclic := []any{"first", nil}
    cyclic[1] = cyclic

    if "[first <cycle>]" != renderTextValue(cyclic) {
        t.Fatalf("expected the slice closing the cycle rendered as the marker, got %q", renderTextValue(cyclic))
    }
}

func TestRenderTextValue_KeepsTheAmpersandOfATopLevelPointerToACyclicMap(t *testing.T) {
    defer boundTextValueStack()()

    cyclic := map[string]any{}
    cyclic["self"] = cyclic

    if "&map[self:<cycle>]" != renderTextValue(&cyclic) {
        t.Fatalf("expected the top-level pointer printed as fmt prints it, got %q", renderTextValue(&cyclic))
    }
}
