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

/* silentReader answers the first read with neither a byte nor an error, which a real network body may legitimately do and no reader used elsewhere in this suite ever does. */
type silentReader struct {
    reads int
    tail  string
}

func (instance *silentReader) Read(target []byte) (int, error) {
    instance.reads = instance.reads + 1

    if 1 == instance.reads {
        return 0, nil
    }

    if "" == instance.tail {
        return 0, io.EOF
    }

    copied := copy(target, instance.tail)
    instance.tail = instance.tail[copied:]

    return copied, nil
}

func (instance *silentReader) Close() error {
    return nil
}

func TestLimitedStreamBody_AProbeThatReturnsNothingDoesNotDecideTheStream(t *testing.T) {
    reader := &silentReader{tail: "x"}

    body := newLimitedStreamBody(reader, 0, "GET", "http://example.com")

    target := make([]byte, 4)

    read, readErr := body.Read(target)
    if nil != readErr {
        t.Fatalf("expected a silent probe to decide nothing, got %v", readErr)
    }

    if 0 != read {
        t.Fatalf("expected no bytes from a silent probe, got %d", read)
    }

    read, readErr = body.Read(target)
    if nil == readErr {
        t.Fatalf("expected the retried probe to find the body past its cap")
    }

    if "response body exceeded max size" != readErr.Error() {
        t.Fatalf("unexpected refusal message: %q", readErr.Error())
    }

    if 0 != read {
        t.Fatalf("expected no bytes past the cap, got %d", read)
    }
}
