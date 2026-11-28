package cli

import (
    "bytes"
    "context"
    "strings"
    "testing"
    "time"

    melodycontainer "github.com/precision-soft/melody/v4/container"
    melodyruntime "github.com/precision-soft/melody/v4/container/runtime"
)

func TestExclusiveTickCommandPrintsIntoTheCommandsWriter(t *testing.T) {
    containerInstance := melodycontainer.NewContainer(melodycontainer.ContainerConfig{})
    t.Cleanup(func() { _ = containerInstance.CloseWithContext(context.Background()) })

    runtimeInstance := melodyruntime.NewRun(context.Background(), containerInstance.NewScope())

    output := &bytes.Buffer{}
    commandContext, parseErr := parseCommandFlags(NewExclusiveTickCommand(), []string{"--hold", "1ms"}, output)
    if nil != parseErr {
        t.Fatalf("expected --hold 1ms to parse, got %v", parseErr)
    }

    if runErr := NewExclusiveTickCommand().Run(runtimeInstance, commandContext); nil != runErr {
        t.Fatalf("expected the tick to complete, got %v", runErr)
    }

    if false == strings.Contains(output.String(), "exclusive tick: started, holding the lock for 1ms") || false == strings.Contains(output.String(), "exclusive tick: done") {
        t.Fatalf("expected both lines in the command's writer, got %q", output.String())
    }
}

func TestExclusiveTickCommand_HoldsTheDeclaredDefaultWhenTheFlagIsLeftOut(t *testing.T) {
    commandContext, parseErr := parseCommandFlags(NewExclusiveTickCommand(), nil, &bytes.Buffer{})
    if nil != parseErr {
        t.Fatalf("expected no flag to parse, got %v", parseErr)
    }

    if hold := commandContext.Duration(exclusiveTickFlagHold); 2*time.Second != hold {
        t.Fatalf("expected the 2s default hold, got %s", hold)
    }
}

func TestExclusiveTickCommand_RefusesAHoldThatIsNotADurationAtTheParse(t *testing.T) {
    for _, value := range []string{"soon", "", " 1s "} {
        if _, parseErr := parseCommandFlags(NewExclusiveTickCommand(), []string{"--hold", value}, &bytes.Buffer{}); nil == parseErr || false == strings.Contains(parseErr.Error(), "for flag -hold") {
            t.Fatalf("expected --hold %q refused at the parse, got %v", value, parseErr)
        }
    }
}
