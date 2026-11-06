package pipeline

import (
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

type HttpMiddlewareFactory func(kernel kernelcontract.Kernel) (httpcontract.Middleware, error)

/* HttpMiddlewareDefinition is one middleware of the pipeline as the builder orders it. Name and Factory are required at Build; a definition with an empty Name is skipped. The builder copies the lists when it takes the definition. */
type HttpMiddlewareDefinition struct {
    Name string

    /* Priority orders the pipeline, the lower first; equal priorities keep registration order. */
    Priority int

    /* Before and After name the middlewares this one is placed ahead of or behind, beyond its priority. */
    Before []string

    After []string

    /* Groups are the pipelines the definition belongs to; nil or empty belongs to every pipeline. */
    Groups []string

    /* EnabledEnvironments restricts the definition to the named environments; nil or empty enables it everywhere. */
    EnabledEnvironments []string

    Factory HttpMiddlewareFactory

    /* ReplaceExisting lets this definition take the place of an earlier one of the same Name; AllowDuplicates admits a second definition of the Name beside the first. */
    ReplaceExisting bool

    AllowDuplicates bool

    /* FunctionName is the function a description names for this definition, the middleware or the factory that builds it, captured at registration so a description needs no factory run; "" when the registrar did not declare it. */
    FunctionName string
}

func copyDefinitionList(values []string) []string {
    if nil == values {
        return nil
    }

    return append(make([]string, 0, len(values)), values...)
}
