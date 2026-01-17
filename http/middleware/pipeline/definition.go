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
