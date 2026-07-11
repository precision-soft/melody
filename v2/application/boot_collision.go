package application

import (
    "fmt"
    goruntime "runtime"
    "sort"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
)

const (
    bootCollisionKindService       = "service"
    bootCollisionKindServiceType   = "serviceType"
    bootCollisionKindParameter     = "parameter"
    bootCollisionKindConfiguration = "configuration"
    bootCollisionKindCliCommand    = "cliCommand"
)

/* bootCollision is one duplicate registration recorded during the boot window instead of panicking immediately, so a consolidation that produced several collisions surfaces them all in one report rather than one panic per boot attempt. The origin carries the registration call site, because by the time the report panics the stack no longer shows where the duplicate came from. */
type bootCollision struct {
    kind   string
    name   string
    origin string
}

/* recordBootCollision defers a duplicate registration to the aggregated report that Boot raises; callerSkip counts stack frames from recordBootCollision's caller to the user's registration call. */
func (instance *Application) recordBootCollision(kind string, name string, callerSkip int) {
    instance.bootCollisions = append(instance.bootCollisions, bootCollision{
        kind:   kind,
        name:   name,
        origin: callerOrigin(callerSkip + 1),
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

func callerOrigin(skip int) string {
    _, file, line, ok := goruntime.Caller(skip + 1)
    if false == ok {
        return "unknown"
    }

    return fmt.Sprintf("%s:%d", file, line)
}
