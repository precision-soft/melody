package httpclient

import (
    "io"
    "strings"
    "testing"
)

type countingCloser struct {
    io.Reader
    closes int
}

func (instance *countingCloser) Close() error {
    instance.closes++

    return nil
}

/* @info A body that ends exactly at the cap is not over it; the buffered path proves the same boundary by reading one byte past, which a stream cannot do without handing the caller bytes it may never want. */
func TestLimitedStreamBody_ABodyEndingAtTheCapIsDelivered(t *testing.T) {
    body := newLimitedStreamBody(io.NopCloser(strings.NewReader("0123456789")), 10, "GET", "https://host/path")

    read, err := io.ReadAll(body)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if "0123456789" != string(read) {
        t.Fatalf("expected the whole body, got %q", string(read))
    }
}

/* @info One byte past the cap fails, and the caller never receives that byte. */
func TestLimitedStreamBody_ABodyPastTheCapFails(t *testing.T) {
    body := newLimitedStreamBody(io.NopCloser(strings.NewReader("0123456789X")), 10, "GET", "https://host/path")

    read, err := io.ReadAll(body)
    if nil == err {
        t.Fatalf("expected the cap to be enforced, got %q", string(read))
    }
    if false == strings.Contains(err.Error(), "exceeded max size") {
        t.Fatalf("expected the failure to name the cap, got %q", err.Error())
    }
    if 10 < len(read) {
        t.Fatalf("expected at most the cap to be delivered, got %d bytes", len(read))
    }
}

/* @info A read larger than what is left of the allowance is clamped rather than overrun. */
func TestLimitedStreamBody_ReadIsClampedToTheRemainingAllowance(t *testing.T) {
    body := newLimitedStreamBody(io.NopCloser(strings.NewReader("0123456789X")), 4, "GET", "https://host/path")

    target := make([]byte, 8)

    read, err := body.Read(target)
    if nil != err {
        t.Fatalf("unexpected error: %v", err)
    }
    if 4 != read {
        t.Fatalf("expected the read to be clamped to the allowance, got %d", read)
    }
}

/* @info Closing the wrapper closes the body it wraps, which is the connection the caller owns. */
func TestLimitedStreamBody_CloseReachesTheWrappedBody(t *testing.T) {
    wrapped := &countingCloser{Reader: strings.NewReader("payload")}

    body := newLimitedStreamBody(wrapped, 10, "GET", "https://host/path")

    if err := body.Close(); nil != err {
        t.Fatalf("unexpected error: %v", err)
    }

    if 1 != wrapped.closes {
        t.Fatalf("expected the wrapped body to be closed once, got %d", wrapped.closes)
    }
}
