package http

import (
    nethttp "net/http"
    "net/http/httptest"
    "testing"

    "github.com/precision-soft/melody/v4/internal/testhelper"
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

func TestExposedRouteAttributes_RefusesAZoneThatIsNotDeclared(t *testing.T) {
    for _, zone := range []string{"unknown", "Frontend", "frontend "} {
        testhelper.AssertPanicsWithError(t, func() {
            _ = ExposedRouteAttributes(zone)
        }, "route zone is not one of the declared zones")
    }
}

func TestRouteProtection_AttributeOfAnotherType_AnswersTheZeroValue(t *testing.T) {
    request := NewRequest(httptest.NewRequest(nethttp.MethodPost, "http://example.com/", nil), nil, RequestConfig{})

    if true == RouteProtection(request).CsrfDisabled || true == RouteProtection(nil).CsrfDisabled {
        t.Fatalf("expected the zero value without the attribute")
    }

    request.Attributes().Set(RouteAttributeProtection, map[string]any{"CsrfDisabled": true})
    if true == RouteProtection(request).CsrfDisabled {
        t.Fatalf("expected a value of another type read as the zero value")
    }

    request.Attributes().Set(RouteAttributeProtection, RouteProtectionConfig{CsrfDisabled: true})
    if false == RouteProtection(request).CsrfDisabled {
        t.Fatalf("expected the carried protection read")
    }
}

func TestRouteProtection_AnswersACopyOfTheHeaderNames(t *testing.T) {
    request := NewRequest(httptest.NewRequest(nethttp.MethodGet, "http://example.com/", nil), nil, RequestConfig{})
    request.Attributes().Set(RouteAttributeProtection, RouteProtectionConfig{SecurityHeadersDisabled: []string{HeaderFrameOptions}})

    RouteProtection(request).SecurityHeadersDisabled[0] = HeaderReferrerPolicy

    if HeaderFrameOptions != RouteProtection(request).SecurityHeadersDisabled[0] {
        t.Fatalf("expected the carried names untouched by a reader")
    }
}
