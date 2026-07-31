package httpclient

import (
    "testing"
)

/* @info The streaming path applies a cap the caller named and leaves an unnamed one to the buffered path, which holds the whole body in memory; the two are told apart by whether the option was ever applied, not by the value it carries. */
func TestRequestOptions_ExplicitMaxResponseBodyBytesIsDistinguishedFromTheDefault(t *testing.T) {
    options := NewRequestOptions()

    if true == options.hasExplicitMaxResponseBodyBytes() {
        t.Fatalf("a fresh option set carries the default, not a caller's cap")
    }
    if 10*1024*1024 != options.MaxResponseBodyBytes() {
        t.Fatalf("unexpected default cap: %d", options.MaxResponseBodyBytes())
    }

    WithMaxResponseBodyBytes(10)(options)

    if false == options.hasExplicitMaxResponseBodyBytes() {
        t.Fatalf("expected an applied option to be recorded as the caller's own")
    }
    if 10 != options.MaxResponseBodyBytes() {
        t.Fatalf("unexpected cap: %d", options.MaxResponseBodyBytes())
    }
}

/* @info A caller who names the same value the default carries still named it, and the streaming path honours it. */
func TestRequestOptions_ExplicitCapEqualToTheDefaultIsStillExplicit(t *testing.T) {
    options := NewRequestOptions()

    WithMaxResponseBodyBytes(10 * 1024 * 1024)(options)

    if false == options.hasExplicitMaxResponseBodyBytes() {
        t.Fatalf("expected a cap equal to the default to count as the caller's own")
    }
}
