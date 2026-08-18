package main

import (
    "bytes"
    "io"
    "net/http"
    "os"
    "strings"
    "testing"
    "time"
)

/* captureStdout redirects os.Stdout for the duration of run and returns what was written. fmt.Printf resolves
os.Stdout at call time, so pass/skip/fail land in the pipe. */
func captureStdout(t *testing.T, run func()) string {
    t.Helper()

    original := os.Stdout

    reader, writer, pipeErr := os.Pipe()
    if nil != pipeErr {
        t.Fatalf("open pipe: %v", pipeErr)
    }

    os.Stdout = writer
    run()
    _ = writer.Close()
    os.Stdout = original

    var buffer bytes.Buffer
    if _, copyErr := io.Copy(&buffer, reader); nil != copyErr {
        t.Fatalf("drain pipe: %v", copyErr)
    }

    return buffer.String()
}

/* the load-balancer half is the ONLY place the trusted-proxy forwarded chain is proven, so an unset
EXAMPLE_LOAD_BALANCER_URL must announce a SKIP rather than return silently: a bare return counted the section
as fully passed and hid a forwarded-chain regression (the false-green class the REDIS_ADDRESS skip guards
against). An empty url short-circuits before any redis/http call, so this exercises the real skip path. */
func TestRunExampleLoadBalancerCheck_AnnouncesSkipWhenUrlEmpty(t *testing.T) {
    output := captureStdout(t, func() {
        runExampleLoadBalancerCheck(&http.Client{Timeout: time.Second}, "", "")
    })

    if false == strings.Contains(output, "SKIP") {
        t.Fatalf("an empty load balancer url must announce a SKIP notice instead of returning silently, got %q", output)
    }
    if false == strings.Contains(output, "EXAMPLE_LOAD_BALANCER_URL") {
        t.Fatalf("the skip notice must name the missing EXAMPLE_LOAD_BALANCER_URL variable, got %q", output)
    }
}
