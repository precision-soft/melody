package cli

import (
    "context"
    "errors"
    "io"
    "strings"
    "testing"
    "time"
)

func TestReadFirstLine_RefusesALinePastTheBound(t *testing.T) {
    if _, readErr := readFirstLine(context.Background(), strings.NewReader(strings.Repeat("a", 9)+"\n"), 8, ""); false == errors.Is(readErr, errFirstLineTooLong) {
        t.Fatalf("expected a line past the bound refused, got %v", readErr)
    }

    if _, readErr := readFirstLine(context.Background(), strings.NewReader(strings.Repeat("a", 1<<20)), 8, ""); false == errors.Is(readErr, errFirstLineTooLong) {
        t.Fatalf("expected an unbroken input refused, got %v", readErr)
    }

    value, readErr := readFirstLine(context.Background(), strings.NewReader("  "+strings.Repeat("a", 8)+"\r\nsecond line\n"), 8, "")
    if nil != readErr || strings.Repeat("a", 8) != value {
        t.Fatalf("expected the trimmed first line at the bound, got %q, %v", value, readErr)
    }
}

func TestReadFirstLine_ReadsNoMoreThanTheBoundAndItsPadding(t *testing.T) {
    input := strings.NewReader(strings.Repeat("a", 1<<20))

    _, _ = readFirstLine(context.Background(), input, 8, "")

    if consumed := (1 << 20) - input.Len(); 8+firstLinePaddingBytes+1 < consumed {
        t.Fatalf("expected at most %d bytes read, read %d", 8+firstLinePaddingBytes+1, consumed)
    }
}

func TestReadFirstLine_GivesUpTheWaitWhenTheContextEnds(t *testing.T) {
    readEnd, writeEnd := io.Pipe()
    t.Cleanup(func() { _ = writeEnd.Close() })

    ctx, cancel := context.WithCancel(context.Background())
    resultChannel := make(chan error, 1)

    go func() {
        _, readErr := readFirstLine(ctx, readEnd, 8, "")
        resultChannel <- readErr
    }()

    cancel()

    select {
    case readErr := <-resultChannel:
        if false == errors.Is(readErr, context.Canceled) {
            t.Fatalf("expected the ended context answered, got %v", readErr)
        }
    case <-time.After(5 * time.Second):
        t.Fatal("expected the read given up once the context ended")
    }
}
