package contract

import (
    "bytes"
    "io"
    "testing"
)

func TestStaticContext_AnswersTheValuesItWasGiven(t *testing.T) {
    buffer := &bytes.Buffer{}

    commandContext := &StaticContext{
        StringValues:      map[string]string{"format": "json"},
        BoolValues:        map[string]bool{"quiet": true},
        IntValues:         map[string]int{"limit": 7},
        StringSliceValues: map[string][]string{"role": {"admin", "editor"}},
        SetFlagNames:      []string{"format"},
        ArgumentValues:    []string{"alpha", "beta"},
        WriterValue:       buffer,
    }

    if "json" != commandContext.String("format") {
        t.Fatalf("expected the string value, got %q", commandContext.String("format"))
    }
    if false == commandContext.Bool("quiet") {
        t.Fatalf("expected the bool value")
    }
    if 7 != commandContext.Int("limit") {
        t.Fatalf("expected the int value, got %d", commandContext.Int("limit"))
    }
    if 2 != len(commandContext.StringSlice("role")) {
        t.Fatalf("expected the slice value, got %v", commandContext.StringSlice("role"))
    }
    if false == commandContext.IsSet("format") {
        t.Fatalf("expected the flag to report as set")
    }
    if true == commandContext.IsSet("quiet") {
        t.Fatalf("expected a value that was not listed as set to report as unset")
    }
    if 2 != len(commandContext.Arguments()) {
        t.Fatalf("expected the arguments, got %v", commandContext.Arguments())
    }
    if buffer != commandContext.Writer() {
        t.Fatalf("expected the given writer")
    }
}

func TestStaticContext_TheZeroValueAnswersZeroesAndDiscards(t *testing.T) {
    commandContext := &StaticContext{}

    if "" != commandContext.String("format") {
        t.Fatalf("expected the empty string")
    }
    if true == commandContext.Bool("quiet") {
        t.Fatalf("expected false")
    }
    if 0 != commandContext.Int("limit") {
        t.Fatalf("expected zero")
    }
    if nil != commandContext.StringSlice("role") {
        t.Fatalf("expected no values")
    }
    if true == commandContext.IsSet("format") {
        t.Fatalf("expected nothing to report as set")
    }
    if nil != commandContext.Arguments() {
        t.Fatalf("expected no arguments")
    }
    if io.Discard != commandContext.Writer() {
        t.Fatalf("expected the discarding writer, so a command's first written line needs no guard")
    }
}

/* the double keeps the contract the parsed context keeps: what a command is handed is its own, or a command that sorts what it was given rewrites what every later reader sees */
func TestStaticContext_AnswersACopyOfWhatItHolds(t *testing.T) {
    commandContext := &StaticContext{
        ArgumentValues:    []string{"alpha"},
        StringSliceValues: map[string][]string{"role": {"admin"}},
    }

    handedArguments := commandContext.Arguments()
    handedArguments[0] = "rewritten"

    handedRoles := commandContext.StringSlice("role")
    handedRoles[0] = "rewritten"

    if "alpha" != commandContext.Arguments()[0] {
        t.Fatalf("expected the arguments to survive the caller's write, got %v", commandContext.Arguments())
    }
    if "admin" != commandContext.StringSlice("role")[0] {
        t.Fatalf("expected the values to survive the caller's write, got %v", commandContext.StringSlice("role"))
    }
}
