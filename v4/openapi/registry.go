package openapi

import (
    "reflect"
    "sync/atomic"

    "github.com/precision-soft/melody/v4/exception"
)

func TypeOf[T any]() reflect.Type {
    return reflect.TypeOf((*T)(nil)).Elem()
}

/* ResponseDescription names one response of a described route under its status, built by Response. */
type ResponseDescription struct {
    Status int
    Type   reflect.Type
}

/* Response answers the description of a response of type T under status, for Descriptor.Responses. */
func Response[T any](status int) ResponseDescription {
    return ResponseDescription{
        Status: status,
        Type:   TypeOf[T](),
    }
}

type Descriptor struct {
    Summary     string
    Description string
    Tags        []string
    RequestType reflect.Type

    /* Responses holds one entry per status, each built by Response; an entry repeating a status replaces the earlier one. */
    Responses []ResponseDescription
}

func NewRegistry() *Registry {
    return &Registry{
        descriptorsByRoute: make(map[string]Descriptor),
    }
}

/* Registry holds the descriptors of every described route. It is written at boot and read on the request path with no lock; MarkServing turns that ordering into a refusal. */
type Registry struct {
    descriptorsByRoute map[string]Descriptor
    serving            atomic.Bool
}

/* Describe records the descriptor of a route and belongs to boot, like the route it describes. After MarkServing it is refused; the check is a flag read before the write, not a lock, so a Describe issued concurrently with Run is the caller's ordering to keep. */
func (instance *Registry) Describe(routeName string, descriptor Descriptor) *Registry {
    instance.refuseDescriptionWhileServing(routeName)

    instance.descriptorsByRoute[routeName] = copyDescriptor(descriptor)
    return instance
}

/* copyDescriptor detaches Tags and Responses, so neither a later write by the caller nor a reader of Get reaches the stored descriptor; a repeated status keeps the place of its first entry and the type of its last. */
func copyDescriptor(descriptor Descriptor) Descriptor {
    copied := descriptor

    if nil != descriptor.Tags {
        copied.Tags = append(make([]string, 0, len(descriptor.Tags)), descriptor.Tags...)
    }

    if nil != descriptor.Responses {
        copied.Responses = make([]ResponseDescription, 0, len(descriptor.Responses))
        positionByStatus := make(map[int]int, len(descriptor.Responses))
        for _, response := range descriptor.Responses {
            if position, seen := positionByStatus[response.Status]; true == seen {
                copied.Responses[position] = response
                continue
            }

            positionByStatus[response.Status] = len(copied.Responses)
            copied.Responses = append(copied.Responses, response)
        }
    }

    return copied
}

/* MarkServing ends the wiring phase: the application calls it from Run, and from then on Describe is refused. A registry never marked keeps admitting descriptions. */
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

/* Get answers a copy of the descriptor, so a reader on the request path cannot write into the registry. */
func (instance *Registry) Get(routeName string) (Descriptor, bool) {
    descriptor, exists := instance.descriptorsByRoute[routeName]
    if false == exists {
        return Descriptor{}, false
    }

    return copyDescriptor(descriptor), true
}
