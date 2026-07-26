package pipeline

import (
    "fmt"
    "strings"
    "testing"

    clockcontract "github.com/precision-soft/melody/v3/clock/contract"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
    containercontract "github.com/precision-soft/melody/v3/container/contract"
    eventcontract "github.com/precision-soft/melody/v3/event/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

/* @info Two definitions may legitimately share a name — that is what allowDuplicates is for, and how one middleware runs both before and after another. The Kahn traversal emits every duplicate, so counting emitted definitions against the node map (which is keyed by unique name) reports a cycle where the graph has none, and Build turns that into an error the application panics on. */
func TestOrderDefinitions_DuplicateNamesAreNotACycle(t *testing.T) {
    first := NewHttpMiddlewareDefinition("audit", 0, nil, nil, nil, nil, nil, false, true)
    second := NewHttpMiddlewareDefinition("audit", 0, nil, nil, nil, nil, nil, false, true)

    ordered, missingReferences, cycleDetected := orderDefinitions([]*HttpMiddlewareDefinition{first, second})

    if true == cycleDetected {
        t.Fatalf("two same-named definitions with no edges form no cycle")
    }
    if 0 != len(missingReferences) {
        t.Fatalf("expected no missing references, got %v", missingReferences)
    }
    if 2 != len(ordered) {
        t.Fatalf("expected both duplicates to survive ordering, got %d", len(ordered))
    }
}

/* @info The sentinel must still catch a real cycle: a depends-on-b and b depends-on-a. */
func TestOrderDefinitions_RealCycleIsDetected(t *testing.T) {
    first := NewHttpMiddlewareDefinition("a", 0, []string{"b"}, nil, nil, nil, nil, false, false)
    second := NewHttpMiddlewareDefinition("b", 0, []string{"a"}, nil, nil, nil, nil, false, false)

    _, _, cycleDetected := orderDefinitions([]*HttpMiddlewareDefinition{first, second})

    if false == cycleDetected {
        t.Fatalf("a mutual before-dependency is a cycle")
    }
}

func TestOrderDefinitions_EqualPriorityKeepsRegistrationOrder(t *testing.T) {
    definitions := make([]*HttpMiddlewareDefinition, 0, 12)
    expected := make([]string, 0, 12)

    for index := 1; index <= 12; index++ {
        name := fmt.Sprintf("middleware.%d.0", index)
        definitions = append(definitions, NewHttpMiddlewareDefinition(name, 0, nil, nil, nil, nil, nil, false, false))
        expected = append(expected, name)
    }

    ordered, _, cycleDetected := orderDefinitions(definitions)

    if true == cycleDetected {
        t.Fatalf("independent definitions form no cycle")
    }
    if len(expected) != len(ordered) {
        t.Fatalf("expected %d definitions, got %d", len(expected), len(ordered))
    }

    for index, definition := range ordered {
        if expected[index] != definition.name {
            t.Fatalf("position %d holds %q, expected %q; full order %v", index, definition.name, expected[index], orderedNames(ordered))
        }
    }
}

func TestOrderDefinitions_EqualPriorityDoesNotFavourFactoriesOverMiddlewares(t *testing.T) {
    registered := NewHttpMiddlewareDefinition("middleware.1.0", 0, nil, nil, nil, nil, nil, false, false)
    fromFactory := NewHttpMiddlewareDefinition("factory.2.0", 0, nil, nil, nil, nil, nil, false, false)

    ordered, _, _ := orderDefinitions([]*HttpMiddlewareDefinition{registered, fromFactory})

    if 2 != len(ordered) {
        t.Fatalf("expected both definitions, got %d", len(ordered))
    }
    if "middleware.1.0" != ordered[0].name {
        t.Fatalf("the first registered definition must stay outermost, got %v", orderedNames(ordered))
    }
}

func TestOrderDefinitions_EdgesStillOverrideRegistrationOrder(t *testing.T) {
    first := NewHttpMiddlewareDefinition("middleware.1.0", 0, []string{}, []string{"middleware.2.0"}, nil, nil, nil, false, false)
    second := NewHttpMiddlewareDefinition("middleware.2.0", 0, nil, nil, nil, nil, nil, false, false)

    ordered, missingReferences, cycleDetected := orderDefinitions([]*HttpMiddlewareDefinition{first, second})

    if true == cycleDetected {
        t.Fatalf("a single after-edge forms no cycle")
    }
    if 0 != len(missingReferences) {
        t.Fatalf("expected no missing references, got %v", missingReferences)
    }
    if "middleware.2.0" != ordered[0].name {
        t.Fatalf("an explicit after-edge must beat registration order, got %v", orderedNames(ordered))
    }
}

func orderedNames(definitions []*HttpMiddlewareDefinition) []string {
    names := make([]string, 0, len(definitions))
    for _, definition := range definitions {
        names = append(names, definition.name)
    }

    return names
}

type gatingTestKernel struct {
    environment string
}

func (instance *gatingTestKernel) Environment() string { return instance.environment }

func (instance *gatingTestKernel) DebugMode() bool { return false }

func (instance *gatingTestKernel) ServiceContainer() containercontract.Container { return nil }

func (instance *gatingTestKernel) EventDispatcher() eventcontract.EventDispatcher { return nil }

func (instance *gatingTestKernel) Config() configcontract.Configuration { return nil }

func (instance *gatingTestKernel) HttpRouter() httpcontract.Router { return nil }

func (instance *gatingTestKernel) HttpKernel() httpcontract.Kernel { return nil }

func (instance *gatingTestKernel) Clock() clockcontract.Clock { return nil }

var _ kernelcontract.Kernel = (*gatingTestKernel)(nil)

func passthroughFactory() HttpMiddlewareFactory {
    return func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
        return func(next httpcontract.Handler) httpcontract.Handler { return next }, nil
    }
}

func buildIn(environment string, definitions ...*HttpMiddlewareDefinition) ([]httpcontract.Middleware, *MiddlewareBuildReport, error) {
    return NewBuilder(definitions...).Build(&gatingTestKernel{environment: environment}, "http")
}

/* A middleware active in every environment that orders itself against a dev-only one boots in dev and refuses to boot in prod: selection drops the dev-only definition, ordering reports the surviving reference as missing, and the application panics. The mistake has to be refused where it is made, so the declared sets are compared and the refusal fires in dev too. */
func TestBuild_RefusesAlwaysOnMiddlewareReferencingEnvironmentGatedOne(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, []string{"profiler"}, nil, []string{"http"}, nil, passthroughFactory(), false, false)
    profiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http"}, []string{"dev"}, passthroughFactory(), false, false)

    _, _, buildErr := buildIn("dev", audit, profiler)
    if nil == buildErr {
        t.Fatalf("an always-on middleware may not reference a dev-only one; the pipeline would refuse to boot in prod")
    }

    message := buildErr.Error()
    if false == strings.Contains(message, "audit") || false == strings.Contains(message, "profiler") {
        t.Fatalf("the error must name both definitions, got: %s", message)
    }
    if false == strings.Contains(message, "dev") {
        t.Fatalf("the error must name the gating that makes the reference unsatisfiable, got: %s", message)
    }
}

/* The refusal is a property of the declarations, not of the environment being booted, so dev and prod must report the same thing. */
func TestBuild_RefusesTheSameReferenceInEveryEnvironment(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, []string{"profiler"}, nil, []string{"http"}, nil, passthroughFactory(), false, false)
    profiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http"}, []string{"dev"}, passthroughFactory(), false, false)

    _, _, devErr := buildIn("dev", audit, profiler)
    _, _, prodErr := buildIn("prod", audit, profiler)

    if nil == devErr || nil == prodErr {
        t.Fatalf("the reference must be refused in both environments, got dev=%v prod=%v", devErr, prodErr)
    }
    if devErr.Error() != prodErr.Error() {
        t.Fatalf("the refusal must not depend on the environment being booted:\n dev: %s\nprod: %s", devErr.Error(), prodErr.Error())
    }
}

/* A dev-only middleware referencing another dev-only one is satisfiable everywhere: wherever the referrer is selected, so is the target. */
func TestBuild_AllowsReferenceWithinTheSameEnvironmentSet(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, []string{"profiler"}, nil, []string{"http"}, []string{"dev"}, passthroughFactory(), false, false)
    profiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http"}, []string{"dev"}, passthroughFactory(), false, false)

    for _, environment := range []string{"dev", "prod"} {
        middlewares, _, buildErr := buildIn(environment, audit, profiler)
        if nil != buildErr {
            t.Fatalf("a dev-only reference to a dev-only middleware is satisfiable, got in %s: %v", environment, buildErr)
        }

        expected := 2
        if "dev" != environment {
            expected = 0
        }
        if expected != len(middlewares) {
            t.Fatalf("expected %d middlewares in %s, got %d", expected, environment, len(middlewares))
        }
    }
}

/* A narrower referrer may reference a broader target: the target is present everywhere the referrer is. */
func TestBuild_AllowsReferenceToBroaderEnvironmentSet(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, []string{"profiler"}, nil, []string{"http"}, []string{"dev"}, passthroughFactory(), false, false)
    profiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http"}, []string{"dev", "prod"}, passthroughFactory(), false, false)

    if _, _, buildErr := buildIn("dev", audit, profiler); nil != buildErr {
        t.Fatalf("a dev-only reference to a dev-and-prod middleware is satisfiable, got: %v", buildErr)
    }
    if _, _, buildErr := buildIn("prod", audit, profiler); nil != buildErr {
        t.Fatalf("the same declarations must build in prod, got: %v", buildErr)
    }
}

/* An empty environment set means every environment, so it covers any referrer. */
func TestBuild_AllowsReferenceToUngatedMiddleware(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, []string{"profiler"}, nil, []string{"http"}, []string{"dev"}, passthroughFactory(), false, false)
    profiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http"}, nil, passthroughFactory(), false, false)

    if _, _, buildErr := buildIn("dev", audit, profiler); nil != buildErr {
        t.Fatalf("a reference to an always-on middleware is satisfiable, got: %v", buildErr)
    }
}

/* The after edge reaches the same ordering pass as the before edge, so it is gated the same way. */
func TestBuild_RefusesGatedReferenceExpressedAsAfter(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, nil, []string{"profiler"}, []string{"http"}, nil, passthroughFactory(), false, false)
    profiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http"}, []string{"dev"}, passthroughFactory(), false, false)

    if _, _, buildErr := buildIn("dev", audit, profiler); nil == buildErr {
        t.Fatalf("an after reference to a dev-only middleware is unsatisfiable just as a before one is")
    }
}

/* Groups gate selection exactly as environments do and fail the same way, so they are compared the same way. */
/* The group a build is asked for decides what that build carries, and several groups are built in one process, so a reference unsatisfiable in some other group says nothing about this one. The selection has already dropped what this group does not carry; a target missing from it is an ordinary missing reference. */
func TestBuild_AllowsReferenceAcrossGroupsTheBuildDoesNotAskFor(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, []string{"profiler"}, nil, []string{"http", "admin"}, nil, passthroughFactory(), false, false)
    profiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http"}, nil, passthroughFactory(), false, false)

    middlewares, _, buildErr := buildIn("dev", audit, profiler)
    if nil != buildErr {
        t.Fatalf("both definitions are selected for the group being built, so the reference resolves, got: %v", buildErr)
    }

    if 2 != len(middlewares) {
        t.Fatalf("expected both middlewares in the built chain, got %d", len(middlewares))
    }
}

func TestBuild_AllowsReferenceToBroaderGroupSet(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, []string{"profiler"}, nil, []string{"http"}, nil, passthroughFactory(), false, false)
    profiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http", "admin"}, nil, passthroughFactory(), false, false)

    if _, _, buildErr := buildIn("dev", audit, profiler); nil != buildErr {
        t.Fatalf("a reference to a middleware registered in more groups is satisfiable, got: %v", buildErr)
    }
}

/* A name no definition carries is an ordinary missing reference; the ordering pass collects every one of them, so that diagnosis must survive untouched. */
func TestBuild_KeepsMissingReferenceErrorForUnknownName(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, []string{"nowhere"}, nil, []string{"http"}, nil, passthroughFactory(), false, false)

    _, report, buildErr := buildIn("dev", audit)
    if nil == buildErr {
        t.Fatalf("a reference to a name no definition carries is still an error")
    }
    if false == strings.Contains(buildErr.Error(), "missing references") {
        t.Fatalf("expected the missing-reference diagnosis, got: %s", buildErr.Error())
    }
    if 1 != len(report.MissingReference()) || "nowhere" != report.MissingReference()[0] {
        t.Fatalf("expected the unknown name in the report, got: %v", report.MissingReference())
    }
}

/* Duplicates registered under one name are each a candidate: one of them covering the referrer is enough, because that one is selected wherever the referrer is. */
func TestBuild_AllowsReferenceWhenOneDuplicateCoversTheReferrer(t *testing.T) {
    audit := NewHttpMiddlewareDefinition("audit", 0, []string{"profiler"}, nil, []string{"http"}, nil, passthroughFactory(), false, false)
    devProfiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http"}, []string{"dev"}, passthroughFactory(), false, true)
    alwaysProfiler := NewHttpMiddlewareDefinition("profiler", 0, nil, nil, []string{"http"}, nil, passthroughFactory(), false, true)

    if _, _, buildErr := buildIn("prod", audit, devProfiler, alwaysProfiler); nil != buildErr {
        t.Fatalf("one always-on definition of the referenced name makes the reference satisfiable, got: %v", buildErr)
    }
}

/* A pipeline with no gating at all — the shape every application registers through Use — must be untouched by the check. */
func TestBuild_LeavesUngatedPipelinesAlone(t *testing.T) {
    first := NewHttpMiddlewareDefinition("middleware.1.0", 0, []string{}, []string{"middleware.2.0"}, []string{"http"}, nil, passthroughFactory(), false, false)
    second := NewHttpMiddlewareDefinition("middleware.2.0", 0, nil, nil, []string{"http"}, nil, passthroughFactory(), false, false)

    middlewares, _, buildErr := buildIn("prod", first, second)
    if nil != buildErr {
        t.Fatalf("unexpected error: %v", buildErr)
    }
    if 2 != len(middlewares) {
        t.Fatalf("expected both middlewares, got %d", len(middlewares))
    }
}
