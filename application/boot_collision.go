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

/* panicOnBootCollisions raises one error naming every duplicate registration recorded during boot; it runs after the cli boot phase, when every module and core registration has happened. */
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
}

func isRegistrationPlumbingFrame(functionName string) bool {
    for _, prefix := range registrationPlumbingFramePrefixes {
        if true == strings.HasPrefix(functionName, prefix) {
            return true
        }
    }

    return false
}
