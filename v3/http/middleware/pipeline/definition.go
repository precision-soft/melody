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
    /* the constraint lists are copied, not retained: the builder reads them at every Build and Describe, so a registrant reusing its slice across two definitions — or mutating it after registration — silently rewrote the ordering constraints the pipeline was registered under. The build report copies its lists for the same reason. */
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
