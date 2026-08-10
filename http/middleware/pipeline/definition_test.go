package pipeline

import (
    "testing"
)

/* @info Builder.Add is the second way definitions reach a builder — NewBuilder takes the first batch, Add takes every later one, which is how a module registers its middlewares after the builder exists. It had no test, so an Add that dropped its argument would leave every module-registered middleware silently out of the pipeline while the boot reported success. */

func TestBuilder_AddAppendsToWhatTheBuilderAlreadyHolds(t *testing.T) {
    first := NewHttpMiddlewareDefinition("security", 10, nil, nil, nil, nil, passthroughFactory(), false, false)
    second := NewHttpMiddlewareDefinition("rate_limit", 20, nil, nil, nil, nil, passthroughFactory(), false, false)

    builder := NewBuilder(first)

    builder.Add(second)

    if 2 != len(builder.definitions) {
        t.Fatalf("expected both definitions to be held, got: %d", len(builder.definitions))
    }

    if "security" != builder.definitions[0].name || "rate_limit" != builder.definitions[1].name {
        t.Fatalf("expected Add to append after what the builder already held")
    }
}

/* @info Add of nothing is a no-op rather than an append of an empty batch: a module that registers conditionally and produces no definition must not disturb the order the definitions already carry. */

func TestBuilder_AddOfNothingLeavesTheBuilderUntouched(t *testing.T) {
    first := NewHttpMiddlewareDefinition("security", 10, nil, nil, nil, nil, passthroughFactory(), false, false)

    builder := NewBuilder(first)

    builder.Add()

    if 1 != len(builder.definitions) {
        t.Fatalf("expected an empty Add to change nothing, got: %d", len(builder.definitions))
    }

    builder.Add([]*HttpMiddlewareDefinition{}...)

    if 1 != len(builder.definitions) {
        t.Fatalf("expected an empty slice spread to change nothing, got: %d", len(builder.definitions))
    }
}

/* @info a definition added after construction reaches the built pipeline, not merely the builder's slice — the assertion that matters to an application is that the middleware actually runs. */

func TestBuilder_AddedDefinitionReachesTheBuiltPipeline(t *testing.T) {
    builder := NewBuilder(
        NewHttpMiddlewareDefinition("security", 10, nil, nil, nil, nil, passthroughFactory(), false, false),
    )

    builder.Add(
        NewHttpMiddlewareDefinition("rate_limit", 20, nil, nil, nil, nil, passthroughFactory(), false, false),
    )

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

/* @info NewInactiveMiddleware is what the build report uses to explain a middleware the operator registered and did not get; both halves of the explanation have to survive, because a reason without a name names nothing. */

func TestNewInactiveMiddleware_CarriesBothHalvesOfTheExplanation(t *testing.T) {
    inactive := NewInactiveMiddleware("compression", "not enabled in this environment")

    if "compression" != inactive.Name() {
        t.Fatalf("unexpected name: %q", inactive.Name())
    }

    if "not enabled in this environment" != inactive.Reason() {
        t.Fatalf("unexpected reason: %q", inactive.Reason())
    }
}

/* the constraint lists are copied at construction: a registrant reusing its slice across definitions — or mutating it after registration — silently rewrote the ordering constraints the pipeline was registered under. */
func TestNewHttpMiddlewareDefinition_CopiesTheConstraintLists(t *testing.T) {
    before := []string{"cors"}

    definition := NewHttpMiddlewareDefinition(
        "candidate",
        0,
        before,
        nil,
        nil,
        nil,
        nil,
        false,
        false,
    )

    before[0] = "session"

    if "cors" != definition.before[0] {
        t.Fatalf("expected the registered constraint untouched, got %q", definition.before[0])
    }
}
