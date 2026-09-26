package cli

import (
    "bytes"
    "context"
    "strings"
    "testing"

    melodycontainer "github.com/precision-soft/melody/v3/container"
    melodyruntime "github.com/precision-soft/melody/v3/runtime"
)

func TestExclusiveTickCommandPrintsIntoTheCommandsWriter(t *testing.T) {
    containerInstance := melodycontainer.NewContainer()
    t.Cleanup(func() { _ = containerInstance.Close() })

    runtimeInstance := melodyruntime.New(context.Background(), containerInstance.NewScope(), containerInstance)

    output := &bytes.Buffer{}
    commandContext := &flagContext{stringByName: map[string]string{"hold": "1ms"}, writer: output}

    if runErr := NewExclusiveTickCommand().Run(runtimeInstance, commandContext); nil != runErr {
        t.Fatalf("expected the tick to complete, got %v", runErr)
    }

    if false == strings.Contains(output.String(), "exclusive tick: started") || false == strings.Contains(output.String(), "exclusive tick: done") {
        t.Fatalf("expected both lines in the command's writer, got %q", output.String())
    }
}
