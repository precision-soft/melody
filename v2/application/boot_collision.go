package application

import (
    "fmt"
    goruntime "runtime"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
)

const (
    bootCollisionKindService           = "service"
    bootCollisionKindServiceType       = "serviceType"
    bootCollisionKindScopedService     = "scopedService"
    bootCollisionKindScopedServiceType = "scopedServiceType"
    bootCollisionKindParameter         = "parameter"
    bootCollisionKindConfiguration     = "configuration"
    bootCollisionKindCliCommand        = "cliCommand"
)

/* bootCollision is one duplicate registration recorded during the boot window, so every collision surfaces in one report. The origin carries the registration call site, or for a collision found inside a boot phase the nearest frame outside the framework, since the stack does not show it when the report panics. */
type bootCollision struct {
    kind   string
    name   string
    origin string
}

/* recordBootCollision defers a duplicate registration to the aggregated report that Boot raises. */
func (instance *Application) recordBootCollision(kind string, name string) {
    instance.bootCollisions = append(instance.bootCollisions, bootCollision{
        kind:   kind,
        name:   name,
        origin: callerOrigin(),
    })
}

/* panicOnBootCollisions raises one error naming every duplicate registration recorded during boot; it runs after the cli boot phase, when every registration channel, the http routes included, has run. */
func (instance *Application) panicOnBootCollisions() {
    if 0 == len(instance.bootCollisions) {
        return
    }

    collisions := make([]string, 0, len(instance.bootCollisions))
    for _, collision := range instance.bootCollisions {
        collisions = append(
            collisions,
            fmt.Sprintf("%s %q registered at %s", collision.kind, collision.name, collision.origin),
        )
    }

    sort.Strings(collisions)

    exception.Panic(
        exception.NewError(
            "duplicate registrations detected at boot",
            exceptioncontract.Context{
                "collisionCount": len(collisions),
                "collisions":     collisions,
            },
            nil,
        ),
    )
}

/* routeCollisionRecorderSetter is the part of a route registry that can defer its duplicate refusals to the aggregated boot report; a registry without it keeps its immediate panic. */
type routeCollisionRecorderSetter interface {
    SetBootCollisionRecorder(recorder func(kind string, name string))
}

/* armRouteCollisionRecorder points the route registry's duplicate refusals at the aggregated report for the boot window; the registry hands over its own kind constants, so the report distinguishes a dispatch-identical route from a name claimed twice. */
func (instance *Application) armRouteCollisionRecorder() {
    setter, isSetter := instance.routeRegistry.(routeCollisionRecorderSetter)
    if false == isSetter {
        return
    }

    setter.SetBootCollisionRecorder(instance.recordBootCollision)
}

func (instance *Application) disarmRouteCollisionRecorder() {
    setter, isSetter := instance.routeRegistry.(routeCollisionRecorderSetter)
    if false == isSetter {
        return
    }

    setter.SetBootCollisionRecorder(nil)
}

/* callerOrigin names the first stack frame outside the framework's own registration plumbing; a fixed frame count would name whichever delegation layer sits in between. */
func callerOrigin() string {
    programCounters := make([]uintptr, 32)
    frameCount := goruntime.Callers(2, programCounters)

    frames := goruntime.CallersFrames(programCounters[:frameCount])

    fallbackOrigin := "unknown"
    isFirstFrame := true

    for {
        frame, more := frames.Next()
        if "" == frame.File {
            break
        }

        if true == isFirstFrame {
            fallbackOrigin = fmt.Sprintf("%s:%d", frame.File, frame.Line)
            isFirstFrame = false
        }

        if false == isRegistrationPlumbingFrame(frame.Function) {
            return fmt.Sprintf("%s:%d", frame.File, frame.Line)
        }

        if false == more {
            break
        }
    }

    return fallbackOrigin
}

var registrationPlumbingFramePrefixes = []string{
    "github.com/precision-soft/melody/v2/application.(*Application).",
    "github.com/precision-soft/melody/v2/application.callerOrigin",
    "github.com/precision-soft/melody/v2/container.",
    /* a duplicate route records from inside the router's registration path, so the origin would otherwise name the route registry instead of the module hook */
    "github.com/precision-soft/melody/v2/http.",
}

func isRegistrationPlumbingFrame(functionName string) bool {
    for _, prefix := range registrationPlumbingFramePrefixes {
        if true == strings.HasPrefix(functionName, prefix) {
            return true
        }
    }

    return false
}
