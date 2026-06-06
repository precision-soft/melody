package messagebus

import (
    "reflect"

    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
)

func NewRouting() *Routing {
    return &Routing{
        routes: make(map[reflect.Type]TransportRouting),
    }
}

type Routing struct {
    routes map[reflect.Type]TransportRouting
}

func RouteType[T any](routing *Routing, name string, transport messagebuscontract.Transport) *Routing {
    routing.routes[reflect.TypeOf((*T)(nil)).Elem()] = TransportRouting{
        Name:      name,
        Transport: transport,
    }

    return routing
}

func (instance *Routing) Build() map[reflect.Type]TransportRouting {
    return instance.routes
}

func NewSendMessageMiddlewareFromRouting(routing *Routing) messagebuscontract.Middleware {
    return NewSendMessageMiddleware(routing.Build())
}
