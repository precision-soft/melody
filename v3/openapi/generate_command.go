package openapi

import (
    "encoding/json"
    "fmt"
    "os"

    clicontract "github.com/precision-soft/melody/v3/cli/contract"
    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/http"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
)

func NewGenerateCommand(info Info, registry *Registry) *GenerateCommand {
    return &GenerateCommand{
        info:     info,
        registry: registry,
    }
}

type GenerateCommand struct {
    info     Info
    registry *Registry
}

func (instance *GenerateCommand) Name() string {
    return "melody:openapi:generate"
}

func (instance *GenerateCommand) Description() string {
    return "generate an OpenAPI 3 document from the registered routes"
}

func (instance *GenerateCommand) Flags() []clicontract.Flag {
    return []clicontract.Flag{
        &clicontract.StringFlag{
            Name:  "out",
            Usage: "path to write the OpenAPI document to; prints to stdout when empty",
        },
    }
}

func (instance *GenerateCommand) Run(
    runtimeInstance runtimecontract.Runtime,
    commandContext *clicontract.CommandContext,
) error {
    router := http.RouterMustFromContainer(runtimeInstance.Container())

    document := Generate(instance.info, router.RouteDefinitions(), instance.registry)

    payload, marshalErr := json.MarshalIndent(document, "", "  ")
    if nil != marshalErr {
        return marshalErr
    }

    out := commandContext.String("out")
    if "" == out {
        fmt.Println(string(payload))
        return nil
    }

    writeErr := os.WriteFile(out, payload, 0o644)
    if nil != writeErr {
        return exception.NewError(
            "could not write the openapi document",
            map[string]any{"out": out},
            writeErr,
        )
    }

    fmt.Println("wrote openapi document to", out)

    return nil
}

var _ clicontract.Command = (*GenerateCommand)(nil)
