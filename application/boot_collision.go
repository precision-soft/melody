package application

import (
    "fmt"
    goruntime "runtime"
    "sort"
    "strings"

    "github.com/precision-soft/melody/exception"
    exceptioncontract "github.com/precision-soft/melody/exception/contract"
)

const (
    bootCollisionKindService           = "service"
    bootCollisionKindServiceType       = "serviceType"
    bootCollisionKindScopedService     = "scopedService"
    bootCollisionKindScopedServiceType = "scopedServiceType"
    bootCollisionKindParameter     = "parameter"
    bootCollisionKindConfiguration = "configuration"
    bootCollisionKindCliCommand    = "cliCommand"
)

/* bootCollision is one duplicate registration recorded during the boot window instead of panicking immediately, so a consolidation that produced several collisions surfaces them all in one report rather than one panic per boot attempt. The origin carries the registration call site, because by the time the report panics the stack no longer shows where the duplicate came from; a collision detected inside a boot phase — a duplicate cli command found while wiring the runner — carries the boot call site instead, the nearest frame that is not the framework's own. */
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

/* panicOnBootCollisions raises one error naming every duplicate registration recorded during boot; it runs after the cli boot phase, when every registration channel — the http routes included, deferred into this report by the recorder Boot arms — has run. */
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

/* routeCollisionRecorderSetter is the part of a route registry that can defer its duplicate refusals to the aggregated boot report. Asked for rather than demanded, the way servingMarker is: a registry double that does not carry it simply keeps its immediate panic — the behavior every registry outside the boot window keeps anyway. */
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

/* callerOrigin names the first stack frame outside the framework's own registration plumbing. A fixed frame count would name whatever delegation layer sits between the user's call and the recording — a count that shifts with every refactor and differs between the name-based and the typed registration path — while the report exists to say where the duplicate came from. */
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
    "github.com/precision-soft/melody/application.(*Application).",
    "github.com/precision-soft/melody/application.callerOrigin",
    "github.com/precision-soft/melody/container.",
    /* a duplicate route records from inside the router's registration path, and without this the origin would name the route registry instead of the module hook that registered the route */
    "github.com/precision-soft/melody/http.",
}

func isRegistrationPlumbingFrame(functionName string) bool {
    for _, prefix := range registrationPlumbingFramePrefixes {
        if true == strings.HasPrefix(functionName, prefix) {
            return true
        }
    }

    return false
}
