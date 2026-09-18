package openapi

import (
    "reflect"
    "sync/atomic"

    "github.com/precision-soft/melody/v3/exception"
)

func TypeOf[T any]() reflect.Type {
    return reflect.TypeOf((*T)(nil)).Elem()
}

type Descriptor struct {
    Summary     string
    Description string
    Tags        []string
    RequestType reflect.Type
    Responses   map[int]reflect.Type
}

func NewRegistry() *Registry {
    return &Registry{
        descriptorsByRoute: make(map[string]Descriptor),
    }
}

/* Registry is a process-lifetime service that holds state — the descriptors of every described route — written at boot and read on the request path, so it belongs to the same class as the router's route tree: a boot registry, frozen once the application serves. The map is plain because every write precedes every read; MarkServing is what makes that ordering a refusal rather than a convention. */
type Registry struct {
    descriptorsByRoute map[string]Descriptor
    serving            atomic.Bool
}

/* Describe records the descriptor of a route. It writes a plain map the spec handler reads on the request path with nothing synchronizing the two, so it belongs to boot — module construction, before the application serves — exactly like the routes it describes. A Describe that STARTS after the application marked the registry serving is refused at the door, the way the router refuses a late route — the door is a flag read before the write, not a lock, so a Describe already past the door when the mark lands still writes: the mark is placed before the listener opens, and a description issued from a goroutine during Run is the caller's ordering to keep. The alternative is a concurrent map write under readers, which Go answers by killing the process, and there is no degraded mode a lock could offer. */
func (instance *Registry) Describe(routeName string, descriptor Descriptor) *Registry {
    instance.refuseDescriptionWhileServing(routeName)

    instance.descriptorsByRoute[routeName] = descriptor
    return instance
}

/* MarkServing records that the wiring phase is over: the application calls it from Run, at the moment it tells the configuration the same thing, and from then on Describe is refused. A registry a test builds by hand and never marks keeps admitting descriptions, which is the honest state of a registry nobody serves from. */
func (instance *Registry) MarkServing() {
    instance.serving.Store(true)
}

func (instance *Registry) refuseDescriptionWhileServing(routeName string) {
    if false == instance.serving.Load() {
        return
    }

    exception.Panic(
        exception.NewError(
            "may not describe a route after the application started serving",
            map[string]any{
                "routeName": routeName,
            },
            nil,
        ),
    )
}

func (instance *Registry) Get(routeName string) (Descriptor, bool) {
    descriptor, exists := instance.descriptorsByRoute[routeName]
    return descriptor, exists
}
