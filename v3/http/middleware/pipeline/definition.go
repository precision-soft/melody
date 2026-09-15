package pipeline

import (
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

type HttpMiddlewareFactory func(kernel kernelcontract.Kernel) (httpcontract.Middleware, error)

type HttpMiddlewareDefinition struct {
    name                string
    priority            int
    before              []string
    after               []string
    groups              []string
    enabledEnvironments []string
    factory             HttpMiddlewareFactory
    replaceExisting     bool
    allowDuplicates     bool

    functionName string
}

/* SetFunctionName records the function a description names for this definition — the registered middleware itself, or the factory that will build it — captured at registration precisely so that listing the pipeline never has to run it */
func (instance *HttpMiddlewareDefinition) SetFunctionName(functionName string) {
    instance.functionName = functionName
}

func NewHttpMiddlewareDefinition(
    name string,
    priority int,
    before []string,
    after []string,
    groups []string,
    enabledEnvironments []string,
    factory HttpMiddlewareFactory,
    replaceExisting bool,
    allowDuplicates bool,
) *HttpMiddlewareDefinition {

    return &HttpMiddlewareDefinition{
        name:                name,
        priority:            priority,
        before:              copyDefinitionList(before),
        after:               copyDefinitionList(after),
        groups:              copyDefinitionList(groups),
        enabledEnvironments: copyDefinitionList(enabledEnvironments),
        factory:             factory,
        replaceExisting:     replaceExisting,
        allowDuplicates:     allowDuplicates,
    }
}

func copyDefinitionList(values []string) []string {
    if nil == values {
        return nil
    }

    return append(make([]string, 0, len(values)), values...)
}
