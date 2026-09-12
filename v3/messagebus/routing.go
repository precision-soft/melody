package messagebus

import (
    "reflect"

    "github.com/precision-soft/melody/v3/exception"
    "github.com/precision-soft/melody/v3/internal"
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
    /* a nil — or typed-nil — transport passes into the routing table here and is dereferenced only later, on the dispatch path, where Send panics far from the wiring that registered it. Refuse it at the registration door in the framed form, the way RegisterTransports refuses a nil entry in its map, so a mis-wired route fails at boot rather than on the first message its type routes. */
    if true == internal.IsNilInterface(transport) {
        exception.Panic(exception.NewError("messagebus route transport is nil", map[string]any{"name": name}, nil))
    }

    routing.routes[reflect.TypeOf((*T)(nil)).Elem()] = TransportRouting{
        Name:      name,
        Transport: transport,
    }

    return routing
}

func (instance *Routing) build() map[reflect.Type]TransportRouting {
    copied := make(map[reflect.Type]TransportRouting, len(instance.routes))
    for key, value := range instance.routes {
        copied[key] = value
    }

    return copied
}

func NewSendMessageMiddlewareFromRouting(routing *Routing) messagebuscontract.Middleware {
    return NewSendMessageMiddleware(routing.build())
}
