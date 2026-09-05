package http

import (
    "testing"

    "github.com/precision-soft/melody/v3/internal/testhelper"
)

func TestRouteZones_DeclaresEveryZoneAndAnswersInDeclarationOrder(t *testing.T) {
    zones := RouteZones()

    expected := []string{RouteZonePublic, RouteZoneInternal, RouteZoneFrontend, RouteZoneClient}
    if len(expected) != len(zones) {
        t.Fatalf("expected %d declared zones, got %v", len(expected), zones)
    }

    for index, zone := range expected {
        if zone != zones[index] {
            t.Fatalf("expected the zones in declaration order, got %v", zones)
        }
    }

    /* the answer is what a command quotes back in its refusal, so it must be a fresh slice: a caller sorting or truncating it would rewrite the accepted spellings for everyone */
    zones[0] = "rewritten"
    if RouteZonePublic != RouteZones()[0] {
        t.Fatalf("expected RouteZones to hand out its own slice, got %v", RouteZones())
    }
}

func TestIsRouteZone_AcceptsExactlyTheDeclaredZones(t *testing.T) {
    for _, zone := range RouteZones() {
        if false == IsRouteZone(zone) {
            t.Fatalf("expected the declared zone %q to be accepted", zone)
        }
    }

    for _, zone := range []string{"", "Public", "public ", "frontEnd", "unknown"} {
        if true == IsRouteZone(zone) {
            t.Fatalf("expected %q not to be a declared zone", zone)
        }
    }
}

func TestExposedRouteAttributes_ExposesWithoutAZoneWhenNoneIsGiven(t *testing.T) {
    attributes := ExposedRouteAttributes("")

    if true != attributes[RouteAttributeExpose] {
        t.Fatalf("expected the route to be opted in, got %v", attributes[RouteAttributeExpose])
    }

    if _, carriesZone := attributes[RouteAttributeZone]; true == carriesZone {
        t.Fatalf("expected no zone attribute when none was asked for, got %v", attributes)
    }
}

func TestExposedRouteAttributes_CarriesADeclaredZone(t *testing.T) {
    attributes := ExposedRouteAttributes(RouteZoneFrontend)

    if true != attributes[RouteAttributeExpose] {
        t.Fatalf("expected the route to be opted in, got %v", attributes[RouteAttributeExpose])
    }

    if RouteZoneFrontend != attributes[RouteAttributeZone] {
        t.Fatalf("expected the zone to be carried, got %v", attributes[RouteAttributeZone])
    }
}

/* a zone that is not declared produces an artifact no filter can ever select: the manifest command compares the requested zone against the route's own string, so a misspelling on the route silently omits it from the zoned export while the unfiltered one still carries it */
func TestExposedRouteAttributes_RefusesAZoneThatIsNotDeclared(t *testing.T) {
    for _, zone := range []string{"unknown", "Frontend", "frontend "} {
        testhelper.AssertPanicsWithError(t, func() {
            _ = ExposedRouteAttributes(zone)
        }, "route zone is not one of the declared zones")
    }
}
