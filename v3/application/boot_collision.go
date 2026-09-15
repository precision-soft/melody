package application

import (
    "fmt"
    goruntime "runtime"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
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

type bootCollision struct {
    kind   string
    name   string
    origin string
}

func (instance *Application) recordBootCollision(kind string, name string) {
    instance.bootCollisions = append(instance.bootCollisions, bootCollision{
        kind:   kind,
        name:   name,
        origin: callerOrigin(),
    })
}

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

type routeCollisionRecorderSetter interface {
    SetBootCollisionRecorder(recorder func(kind string, name string))
}

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
    "github.com/precision-soft/melody/v3/application.(*Application).",
    "github.com/precision-soft/melody/v3/application.callerOrigin",
    "github.com/precision-soft/melody/v3/container.",

    "github.com/precision-soft/melody/v3/http.",
}

func isRegistrationPlumbingFrame(functionName string) bool {
    for _, prefix := range registrationPlumbingFramePrefixes {
        if true == strings.HasPrefix(functionName, prefix) {
            return true
        }
    }

    return false
}
