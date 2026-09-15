package cli

import (
    "bytes"
    "errors"
    "strings"
    "testing"
)

func TestExclusiveTickCommandUsesRunnerWriter(t *testing.T) {
    buffer := &bytes.Buffer{}
    runErr := NewExclusiveTickCommand().Run(newCommandFixture(t).runtime, &flagContext{stringByName: map[string]string{"hold": "0s"}, writer: buffer})
    if nil != runErr || false == strings.Contains(buffer.String(), "exclusive tick: started") || false == strings.Contains(buffer.String(), "exclusive tick: done") {
        t.Fatalf("output=%q error=%v", buffer.String(), runErr)
    }
}

func TestExclusiveTickCommandReturnsWriterFailure(t *testing.T) {
    failure := errors.New("writer closed")
    runErr := NewExclusiveTickCommand().Run(newCommandFixture(t).runtime, &flagContext{stringByName: map[string]string{"hold": "0s"}, writer: &failedCommandWriter{failure: failure}})
    if false == errors.Is(runErr, failure) {
        t.Fatalf("writer failure lost: %v", runErr)
    }
}
