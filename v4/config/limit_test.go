package config

import (
    "testing"
    "time"

    "github.com/precision-soft/melody/v4/bound"
    "github.com/precision-soft/melody/v4/exception"
)

func TestResolveUnavailableRetryAfter_DecidesTheDelayAgainstItsRule(t *testing.T) {
    for name, testCase := range map[string]struct {
        limit    bound.Duration
        expected time.Duration
    }{
        "absent":       {limit: bound.Duration{}, expected: 5 * time.Second},
        "the minimum":  {limit: bound.Of(time.Second), expected: time.Second},
        "the maximum":  {limit: bound.Of(time.Hour), expected: time.Hour},
        "whole minute": {limit: bound.Of(time.Minute), expected: time.Minute},
    } {
        actual, resolveErr := ResolveUnavailableRetryAfter(testCase.limit, "Field")
        if nil != resolveErr || testCase.expected != actual {
            t.Fatalf("%s: expected %s, got %s, %v", name, testCase.expected, actual, resolveErr)
        }
    }

    for name, limit := range map[string]bound.Duration{
        "0s":        bound.Of(time.Duration(0)),
        "999ms":     bound.Of(999 * time.Millisecond),
        "1500ms":    bound.Of(1500 * time.Millisecond),
        "1h1s":      bound.Of(time.Hour + time.Second),
        "unlimited": bound.UnlimitedDuration(),
    } {
        _, resolveErr := ResolveUnavailableRetryAfter(limit, "Field")
        if nil == resolveErr {
            t.Fatalf("%s: expected a refusal", name)
        }

        if "Field" != exception.LogContext(resolveErr, exception.LogContextConfig{})["field"] {
            t.Fatalf("%s: expected the refusal to name the field, got %v", name, resolveErr)
        }
    }
}
