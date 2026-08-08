package pipeline

import (
    httpcontract "github.com/precision-soft/melody/http/contract"
    kernelcontract "github.com/precision-soft/melody/kernel/contract"
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
    /* the function behind the definition, captured at registration so a description needs no factory run; empty when the registrar did not declare it */
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
        before:              before,
        after:               after,
        groups:              groups,
        enabledEnvironments: enabledEnvironments,
        factory:             factory,
        replaceExisting:     replaceExisting,
        allowDuplicates:     allowDuplicates,
    }
}
