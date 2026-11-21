package cli

import (
    "bufio"
    "context"
    "errors"
    "fmt"
    "io"
    "os"
    "strings"

    "golang.org/x/term"
)

var errFirstLineTooLong = errors.New("the first line of the input is longer than its bound")

/* firstLinePaddingBytes is the whitespace around the value (a carriage return, a stray blank) that is read past the bound and then trimmed away */
const firstLinePaddingBytes = 16

type firstLineResult struct {
    value string
    err   error
}

/* readFirstLine answers the first line of the input, trimmed, and gives up the wait when the context ends; on a terminal it names what it waits for on standard error and reads with the echo off, so a typed secret never shows */
func readFirstLine(ctx context.Context, input io.Reader, maximumBytes int, prompt string) (string, error) {
    if file, isFile := input.(*os.File); true == isFile && true == term.IsTerminal(int(file.Fd())) {
        return readTerminalLine(ctx, file, maximumBytes, prompt)
    }

    return awaitFirstLine(ctx, func() (string, error) {
        return readBoundedLine(input, maximumBytes)
    }, func() {})
}

/* the read runs on its own goroutine since a blocked read cannot be interrupted; on an ended context it is left behind, the process exiting with the command */
func awaitFirstLine(ctx context.Context, read func() (string, error), onInterrupt func()) (string, error) {
    resultChannel := make(chan firstLineResult, 1)

    go func() {
        value, readErr := read()
        resultChannel <- firstLineResult{value: value, err: readErr}
    }()

    select {
    case result := <-resultChannel:
        return result.value, result.err
    case <-ctx.Done():
        onInterrupt()

        return "", fmt.Errorf("the input was not read: %w", ctx.Err())
    }
}

func readTerminalLine(ctx context.Context, file *os.File, maximumBytes int, prompt string) (string, error) {
    fileDescriptor := int(file.Fd())

    state, stateErr := term.GetState(fileDescriptor)
    if nil != stateErr {
        return "", stateErr
    }

    _, _ = fmt.Fprint(os.Stderr, prompt)

    /* the echo is turned back on when the wait is given up, since the read left behind would otherwise leave the shell without it */
    value, readErr := awaitFirstLine(ctx, func() (string, error) {
        line, passwordErr := term.ReadPassword(fileDescriptor)

        return string(line), passwordErr
    }, func() {
        _ = term.Restore(fileDescriptor, state)
    })

    _, _ = fmt.Fprintln(os.Stderr)

    if nil != readErr {
        return "", readErr
    }

    value = strings.TrimSpace(value)
    if maximumBytes < len(value) {
        return "", errFirstLineTooLong
    }

    return value, nil
}

/* readBoundedLine never reads more than the bound and its padding, so an input without a line break cannot grow the read without limit */
func readBoundedLine(input io.Reader, maximumBytes int) (string, error) {
    readLimit := maximumBytes + firstLinePaddingBytes

    line, readErr := bufio.NewReader(io.LimitReader(input, int64(readLimit+1))).ReadString('\n')
    if nil != readErr && false == errors.Is(readErr, io.EOF) {
        return "", readErr
    }

    if readLimit < len(strings.TrimSuffix(line, "\n")) {
        return "", errFirstLineTooLong
    }

    value := strings.TrimSpace(line)
    if maximumBytes < len(value) {
        return "", errFirstLineTooLong
    }

    return value, nil
}
