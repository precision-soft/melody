package application

import (
    "fmt"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    "github.com/precision-soft/melody/v4/security"
)

/* firewallDoorRouter is the part of a router that can publish a definition no handler answers and say which routes claim a path. */
type firewallDoorRouter interface {
    DeclareRouteDefinition(pattern string, options httpcontract.RouteOptions)

    RoutesClaimingPath(path string) []httpcontract.RouteDefinition
}

func (instance *Application) firewallDoors() []security.FirewallDoor {
    if nil == instance.securityConfiguration {
        return nil
    }

    doors := make([]security.FirewallDoor, 0)
    for _, firewall := range instance.securityConfiguration.Firewalls() {
        doors = append(doors, firewall.Doors()...)
    }

    return doors
}

func (instance *Application) firewallDoorRouter() firewallDoorRouter {
    router, isDoorRouter := instance.kernel.HttpRouter().(firewallDoorRouter)
    if false == isDoorRouter {
        exception.Panic(
            exception.NewError(
                "the http router cannot publish a firewall door's route definition",
                exceptioncontract.Context{"routerType": fmt.Sprintf("%T", instance.kernel.HttpRouter())},
                nil,
            ),
        )
    }

    return router
}

/* declareFirewallDoorRoutes publishes each firewall door as a named definition, so the url generator and the manifest resolve it. */
func (instance *Application) declareFirewallDoorRoutes() {
    doors := instance.firewallDoors()
    if 0 == len(doors) {
        return
    }

    router := instance.firewallDoorRouter()

    for _, door := range doors {
        router.DeclareRouteDefinition(
            door.Path,
            httpcontract.RouteOptions{Name: door.Name, Methods: door.Methods, Attributes: door.Attributes},
        )
    }
}

/* recordFirewallDoorRouteCollisions reports every route that claims a door's path, whatever its method: the door answers its own methods before routing matters, and a route beside it would answer the others. It reads the routes once every registration channel ran. */
func (instance *Application) recordFirewallDoorRouteCollisions() {
    doors := instance.firewallDoors()
    if 0 == len(doors) {
        return
    }

    router := instance.firewallDoorRouter()

    for _, door := range doors {
        for _, definition := range router.RoutesClaimingPath(door.Path) {
            instance.recordBootCollision(
                bootCollisionKindFirewallDoorRoute,
                fmt.Sprintf("route %q (%s) on the door %q (%s)", definition.Name(), definition.Pattern(), door.Name, door.Path),
            )
        }
    }
}
