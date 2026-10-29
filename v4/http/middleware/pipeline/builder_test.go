package pipeline

import (
    "fmt"
    "strings"
    "testing"

    clockcontract "github.com/precision-soft/melody/v4/clock/contract"
    "github.com/precision-soft/melody/v4/config"
    configcontract "github.com/precision-soft/melody/v4/config/contract"
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    eventcontract "github.com/precision-soft/melody/v4/event/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

/* Two definitions may legitimately share a name — that is what allowDuplicates is for, and how one middleware runs both before and after another. The Kahn traversal emits every duplicate, so counting emitted definitions against the node map (which is keyed by unique name) reports a cycle where the graph has none, and Build turns that into an error the application panics on. */
func TestOrderDefinitions_DuplicateNamesAreNotACycle(t *testing.T) {
    first := HttpMiddlewareDefinition{Name: "audit", AllowDuplicates: true}
    second := HttpMiddlewareDefinition{Name: "audit", AllowDuplicates: true}

    ordered, missingReferences, cycleDetected := orderDefinitions([]*HttpMiddlewareDefinition{&first, &second})

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

/* The sentinel must still catch a real cycle: a depends-on-b and b depends-on-a. */
func TestOrderDefinitions_RealCycleIsDetected(t *testing.T) {
    first := HttpMiddlewareDefinition{Name: "a", Before: []string{"b"}}
    second := HttpMiddlewareDefinition{Name: "b", Before: []string{"a"}}

    _, _, cycleDetected := orderDefinitions([]*HttpMiddlewareDefinition{&first, &second})

    if false == cycleDetected {
        t.Fatalf("a mutual before-dependency is a cycle")
    }
}

func TestOrderDefinitions_EqualPriorityKeepsRegistrationOrder(t *testing.T) {
    definitions := make([]*HttpMiddlewareDefinition, 0, 12)
    expected := make([]string, 0, 12)

    for index := 1; index <= 12; index++ {
        name := fmt.Sprintf("middleware.%d.0", index)
        definitions = append(definitions, &HttpMiddlewareDefinition{Name: name})
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
        if expected[index] != definition.Name {
            t.Fatalf("position %d holds %q, expected %q; full order %v", index, definition.Name, expected[index], orderedNames(ordered))
        }
    }
}

func TestOrderDefinitions_EqualPriorityDoesNotFavourFactoriesOverMiddlewares(t *testing.T) {
    registered := HttpMiddlewareDefinition{Name: "middleware.1.0"}
    fromFactory := HttpMiddlewareDefinition{Name: "factory.2.0"}

    ordered, _, _ := orderDefinitions([]*HttpMiddlewareDefinition{&registered, &fromFactory})

    if 2 != len(ordered) {
        t.Fatalf("expected both definitions, got %d", len(ordered))
    }
    if "middleware.1.0" != ordered[0].Name {
        t.Fatalf("the first registered definition must stay outermost, got %v", orderedNames(ordered))
    }
}

func TestOrderDefinitions_EdgesStillOverrideRegistrationOrder(t *testing.T) {
    first := HttpMiddlewareDefinition{Name: "middleware.1.0", Before: []string{}, After: []string{"middleware.2.0"}}
    second := HttpMiddlewareDefinition{Name: "middleware.2.0"}

    ordered, missingReferences, cycleDetected := orderDefinitions([]*HttpMiddlewareDefinition{&first, &second})

    if true == cycleDetected {
        t.Fatalf("a single after-edge forms no cycle")
    }
    if 0 != len(missingReferences) {
        t.Fatalf("expected no missing references, got %v", missingReferences)
    }
    if "middleware.2.0" != ordered[0].Name {
        t.Fatalf("an explicit after-edge must beat registration order, got %v", orderedNames(ordered))
    }
}

func orderedNames(definitions []*HttpMiddlewareDefinition) []string {
    names := make([]string, 0, len(definitions))
    for _, definition := range definitions {
        names = append(names, definition.Name)
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

func buildIn(environment string, definitions ...HttpMiddlewareDefinition) ([]httpcontract.Middleware, *MiddlewareBuildReport, error) {
    return NewBuilder(definitions).Build(&gatingTestKernel{environment: environment}, "http")
}

/* A middleware active in every environment that orders itself against a dev-only one boots in dev and refuses to boot in prod: selection drops the dev-only definition, ordering reports the surviving reference as missing, and the application panics. The mistake has to be refused where it is made, so the declared sets are compared and the refusal fires in dev too. */
func TestBuild_RefusesAlwaysOnMiddlewareReferencingEnvironmentGatedOne(t *testing.T) {
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"profiler"}, Groups: []string{"http"}, Factory: passthroughFactory()}
    profiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory()}

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
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"profiler"}, Groups: []string{"http"}, Factory: passthroughFactory()}
    profiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory()}

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
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"profiler"}, Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory()}
    profiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory()}

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
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"profiler"}, Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory()}
    profiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http"}, EnabledEnvironments: []string{"dev", "prod"}, Factory: passthroughFactory()}

    if _, _, buildErr := buildIn("dev", audit, profiler); nil != buildErr {
        t.Fatalf("a dev-only reference to a dev-and-prod middleware is satisfiable, got: %v", buildErr)
    }
    if _, _, buildErr := buildIn("prod", audit, profiler); nil != buildErr {
        t.Fatalf("the same declarations must build in prod, got: %v", buildErr)
    }
}

/* An empty environment set means every environment, so it covers any referrer. */
func TestBuild_AllowsReferenceToUngatedMiddleware(t *testing.T) {
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"profiler"}, Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory()}
    profiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http"}, Factory: passthroughFactory()}

    if _, _, buildErr := buildIn("dev", audit, profiler); nil != buildErr {
        t.Fatalf("a reference to an always-on middleware is satisfiable, got: %v", buildErr)
    }
}

/* The after edge reaches the same ordering pass as the before edge, so it is gated the same way. */
func TestBuild_RefusesGatedReferenceExpressedAsAfter(t *testing.T) {
    audit := HttpMiddlewareDefinition{Name: "audit", After: []string{"profiler"}, Groups: []string{"http"}, Factory: passthroughFactory()}
    profiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory()}

    if _, _, buildErr := buildIn("dev", audit, profiler); nil == buildErr {
        t.Fatalf("an after reference to a dev-only middleware is unsatisfiable just as a before one is")
    }
}

/* The group a build is asked for decides what that build carries, and several groups are built in one process, so a reference unsatisfiable in some other group says nothing about this one. The selection has already dropped what this group does not carry; a target missing from it is an ordinary missing reference. */
func TestBuild_AllowsReferenceAcrossGroupsTheBuildDoesNotAskFor(t *testing.T) {
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"profiler"}, Groups: []string{"http", "admin"}, Factory: passthroughFactory()}
    profiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http"}, Factory: passthroughFactory()}

    middlewares, _, buildErr := buildIn("dev", audit, profiler)
    if nil != buildErr {
        t.Fatalf("both definitions are selected for the group being built, so the reference resolves, got: %v", buildErr)
    }

    if 2 != len(middlewares) {
        t.Fatalf("expected both middlewares in the built chain, got %d", len(middlewares))
    }
}

func TestBuild_AllowsReferenceToBroaderGroupSet(t *testing.T) {
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"profiler"}, Groups: []string{"http"}, Factory: passthroughFactory()}
    profiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http", "admin"}, Factory: passthroughFactory()}

    if _, _, buildErr := buildIn("dev", audit, profiler); nil != buildErr {
        t.Fatalf("a reference to a middleware registered in more groups is satisfiable, got: %v", buildErr)
    }
}

/* A name no definition carries is an ordinary missing reference; the ordering pass collects every one of them, so that diagnosis must survive untouched. */
func TestBuild_KeepsMissingReferenceErrorForUnknownName(t *testing.T) {
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"nowhere"}, Groups: []string{"http"}, Factory: passthroughFactory()}

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
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"profiler"}, Groups: []string{"http"}, Factory: passthroughFactory()}
    devProfiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory(), AllowDuplicates: true}
    alwaysProfiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"http"}, Factory: passthroughFactory(), AllowDuplicates: true}

    if _, _, buildErr := buildIn("prod", audit, devProfiler, alwaysProfiler); nil != buildErr {
        t.Fatalf("one always-on definition of the referenced name makes the reference satisfiable, got: %v", buildErr)
    }
}

/* A pipeline with no gating at all — the shape every application registers through Use — must be untouched by the check. */
func TestBuild_LeavesUngatedPipelinesAlone(t *testing.T) {
    first := HttpMiddlewareDefinition{Name: "middleware.1.0", Before: []string{}, After: []string{"middleware.2.0"}, Groups: []string{"http"}, Factory: passthroughFactory()}
    second := HttpMiddlewareDefinition{Name: "middleware.2.0", Groups: []string{"http"}, Factory: passthroughFactory()}

    middlewares, _, buildErr := buildIn("prod", first, second)
    if nil != buildErr {
        t.Fatalf("unexpected error: %v", buildErr)
    }
    if 2 != len(middlewares) {
        t.Fatalf("expected both middlewares, got %d", len(middlewares))
    }
}

/* Splitting one middleware across environments is how a configuration that differs per environment is ordinarily written: an `auth` wired for development beside an `auth` wired for production, both registered under the one name the rest of the pipeline orders against. What has to be present wherever the referrer runs is A middleware called `auth`, not one particular registration of it — so the union of the registrations is what answers the reference. Weighing them one at a time refused this in dev AND in prod, for a configuration that boots correctly in both. */
func TestBuild_AcceptsAReferenceCoveredByTheUnionOfSameNamedDefinitions(t *testing.T) {
    developmentAuth := HttpMiddlewareDefinition{Name: "auth", Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory(), AllowDuplicates: true}
    productionAuth := HttpMiddlewareDefinition{Name: "auth", Groups: []string{"http"}, EnabledEnvironments: []string{"prod"}, Factory: passthroughFactory(), AllowDuplicates: true}
    audit := HttpMiddlewareDefinition{Name: "audit", After: []string{"auth"}, Groups: []string{"http"}, Factory: passthroughFactory()}

    for _, environment := range []string{"dev", "prod"} {
        if _, _, buildErr := buildIn(environment, developmentAuth, productionAuth, audit); nil != buildErr {
            t.Fatalf("environment %q: the two auth registrations cover every environment between them, so the reference is satisfiable: %v", environment, buildErr)
        }
    }
}

/* the control: a union that still leaves a gap is still refused. Two registrations covering dev and staging do not answer an always-on referrer, which runs in production too, and that is the mistake the check exists to catch. */
func TestBuild_StillRefusesAReferenceTheUnionDoesNotCover(t *testing.T) {
    developmentAuth := HttpMiddlewareDefinition{Name: "auth", Groups: []string{"http"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory(), AllowDuplicates: true}
    stagingAuth := HttpMiddlewareDefinition{Name: "auth", Groups: []string{"http"}, EnabledEnvironments: []string{"staging"}, Factory: passthroughFactory(), AllowDuplicates: true}
    audit := HttpMiddlewareDefinition{Name: "audit", After: []string{"auth"}, Groups: []string{"http"}, Factory: passthroughFactory()}

    _, _, buildErr := buildIn("dev", developmentAuth, stagingAuth, audit)
    if nil == buildErr {
        t.Fatal("an always-on referrer is not covered by registrations confined to dev and staging; production carries none of them")
    }
}

/* supportedEnvironments is a copy of what config.validateEnvironment admits, and the union of same-named definitions is only universal because of it: a name registered for every supported environment is registered everywhere. If config ever admits a third environment and this list does not learn about it, the union silently stops covering an always-on referrer and a configuration that boots everywhere starts being refused. The comment above the copy promises this guard; here it is. */
func TestSupportedEnvironments_MatchesTheConfigurationPackage(t *testing.T) {
    fromConfiguration := []string{config.EnvDevelopment, config.EnvProduction}

    if len(fromConfiguration) != len(supportedEnvironments) {
        t.Fatalf("the pipeline knows %d environments and config admits %d: %v vs %v", len(supportedEnvironments), len(fromConfiguration), supportedEnvironments, fromConfiguration)
    }

    for _, environment := range fromConfiguration {
        found := false
        for _, known := range supportedEnvironments {
            if known == environment {
                found = true
            }
        }

        if false == found {
            t.Fatalf("config admits the environment %q and the pipeline does not know it, so the union of same-named definitions can no longer cover an always-on reference", environment)
        }
    }
}

/* a pair of definitions confined to `api` does not refuse the `web` build, which assembles neither; the check still fires for the group that carries the pair */
func TestBuild_GatingOfAnotherGroupDoesNotRefuseThisOne(t *testing.T) {
    profiler := HttpMiddlewareDefinition{Name: "profiler", Groups: []string{"api"}, EnabledEnvironments: []string{"dev"}, Factory: passthroughFactory()}
    audit := HttpMiddlewareDefinition{Name: "audit", Before: []string{"profiler"}, Groups: []string{"api"}, Factory: passthroughFactory()}
    page := HttpMiddlewareDefinition{Name: "page", Groups: []string{"web"}, Factory: passthroughFactory()}

    if _, _, buildErr := NewBuilder([]HttpMiddlewareDefinition{profiler, audit, page}).Build(&gatingTestKernel{environment: "dev"}, "web"); nil != buildErr {
        t.Fatalf("the web group assembles neither definition of the api pair, so their gating cannot make it unbuildable: %v", buildErr)
    }

    if _, _, buildErr := NewBuilder([]HttpMiddlewareDefinition{profiler, audit, page}).Build(&gatingTestKernel{environment: "dev"}, "api"); nil == buildErr {
        t.Fatal("the api group does carry the pair, so the unsatisfiable reference must still be refused there")
    }
}

/* Describe answers what Build would run, so it mirrors Build's own third refusal: without it the listing reports as healthy a definition the serving boot refuses. */
func TestBuilderDescribe_MirrorsTheNilFactoryRefusalOfBuild(t *testing.T) {
    builder := NewBuilder([]HttpMiddlewareDefinition{{Name: "broken"}})

    _, _, describeErr := builder.Describe("dev", "")
    if nil == describeErr || false == strings.Contains(describeErr.Error(), "middleware factory is nil") {
        t.Fatalf("expected the description to refuse the definition the build refuses, got %v", describeErr)
    }
}

func TestBuilder_KeepsTheReportWhenAFactoryFails(t *testing.T) {
    builder := NewBuilder([]HttpMiddlewareDefinition{
        HttpMiddlewareDefinition{
            Name:     "failing",
            Priority: 10,
            Factory: func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                return nil, fmt.Errorf("factory exploded")
            },
        },
    })

    _, report, buildErr := builder.Build(&gatingTestKernel{environment: "dev"}, "")
    if nil == buildErr {
        t.Fatalf("expected the factory failure to surface")
    }

    /* the report travels with the refusal, so the caller reporting the failure has the selection, the gating and the cycle detection around the offending middleware, as Describe — the same selection without the factories — returns them */
    if nil == report {
        t.Fatalf("expected the report to travel with the refusal")
    }
}

func TestBuilder_RecordsAMiddlewareItsFactoryDeclinedToBuildAsInactive(t *testing.T) {
    builder := NewBuilder([]HttpMiddlewareDefinition{
        HttpMiddlewareDefinition{
            Name:     "declined",
            Priority: 10,
            Factory: func(kernelInstance kernelcontract.Kernel) (httpcontract.Middleware, error) {
                return nil, nil
            },
        },
    })

    middlewares, report, buildErr := builder.Build(&gatingTestKernel{environment: "dev"}, "")
    if nil != buildErr {
        t.Fatalf("build: %v", buildErr)
    }

    if 0 != len(middlewares) {
        t.Fatalf("expected no middleware in the chain, got %d", len(middlewares))
    }

    /* requested, selected and ordered, then vanished from the chain without appearing in the selected names OR in the inactive list — the operator reading the report saw a pipeline that simply never mentioned it */
    found := false
    for _, inactive := range report.Inactive() {
        if "declined" == inactive.Name() {
            found = true
        }
    }

    if false == found {
        t.Fatalf("expected the declined middleware to be reported inactive, got %+v", report.Inactive())
    }

    for _, selectedName := range report.SelectedNames() {
        if "declined" == selectedName {
            t.Fatalf("a middleware that was never built must not be reported as selected")
        }
    }
}

func TestBuilder_AddAppendsToWhatTheBuilderAlreadyHolds(t *testing.T) {
    first := HttpMiddlewareDefinition{Name: "security", Priority: 10, Factory: passthroughFactory()}
    second := HttpMiddlewareDefinition{Name: "rate_limit", Priority: 20, Factory: passthroughFactory()}

    builder := NewBuilder([]HttpMiddlewareDefinition{first})

    builder.Add([]HttpMiddlewareDefinition{second})

    if 2 != len(builder.definitions) {
        t.Fatalf("expected both definitions to be held, got: %d", len(builder.definitions))
    }

    if "security" != builder.definitions[0].Name || "rate_limit" != builder.definitions[1].Name {
        t.Fatalf("expected Add to append after what the builder already held")
    }
}

func TestBuilder_AddOfNothingLeavesTheBuilderUntouched(t *testing.T) {
    first := HttpMiddlewareDefinition{Name: "security", Priority: 10, Factory: passthroughFactory()}

    builder := NewBuilder([]HttpMiddlewareDefinition{first})

    builder.Add(nil)

    if 1 != len(builder.definitions) {
        t.Fatalf("expected an empty Add to change nothing, got: %d", len(builder.definitions))
    }

    builder.Add([]HttpMiddlewareDefinition{})

    if 1 != len(builder.definitions) {
        t.Fatalf("expected an empty slice to change nothing, got: %d", len(builder.definitions))
    }
}

func TestBuilder_AddedDefinitionReachesTheBuiltPipeline(t *testing.T) {
    builder := NewBuilder([]HttpMiddlewareDefinition{
        HttpMiddlewareDefinition{Name: "security", Priority: 10, Factory: passthroughFactory()},
    })

    builder.Add([]HttpMiddlewareDefinition{
        HttpMiddlewareDefinition{Name: "rate_limit", Priority: 20, Factory: passthroughFactory()},
    })

    middlewares, report, err := builder.Build(&gatingTestKernel{environment: "dev"}, "")
    if nil != err {
        t.Fatalf("unexpected build error: %v", err)
    }

    if 2 != len(middlewares) {
        t.Fatalf("expected both middlewares in the pipeline, got: %d", len(middlewares))
    }

    if 2 != len(report.SelectedNames()) {
        t.Fatalf("expected both names in the report, got: %v", report.SelectedNames())
    }
}

/* the builder copies the constraint lists when it takes a definition: a registrant reusing its slice across definitions — or mutating it after registration — would otherwise rewrite the ordering constraints the pipeline was registered under. */
func TestBuilder_CopiesTheListsWhenItTakesADefinition(t *testing.T) {
    before := []string{"cors"}
    after := []string{"session"}
    groups := []string{"http"}
    enabledEnvironments := []string{"dev"}

    builder := NewBuilder([]HttpMiddlewareDefinition{{Name: "candidate", Before: before}})
    builder.Add([]HttpMiddlewareDefinition{{Name: "second", After: after, Groups: groups, EnabledEnvironments: enabledEnvironments}})

    before[0] = "x"
    after[0] = "x"
    groups[0] = "x"
    enabledEnvironments[0] = "x"

    if "cors" != builder.definitions[0].Before[0] {
        t.Fatalf("expected the registered before list untouched, got %q", builder.definitions[0].Before[0])
    }

    taken := builder.definitions[1]
    if "session" != taken.After[0] || "http" != taken.Groups[0] || "dev" != taken.EnabledEnvironments[0] {
        t.Fatalf("expected the registered lists untouched, got %v %v %v", taken.After, taken.Groups, taken.EnabledEnvironments)
    }
}

func TestNewBuilder_NilTakesNone(t *testing.T) {
    middlewares, _, buildErr := NewBuilder(nil).Build(&gatingTestKernel{environment: "dev"}, "")
    if nil != buildErr || 0 != len(middlewares) {
        t.Fatalf("expected an empty pipeline, got %d middlewares and %v", len(middlewares), buildErr)
    }
}
