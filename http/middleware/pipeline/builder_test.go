package pipeline

import (
    "testing"
)

/** @info Two definitions may legitimately share a name — that is what allowDuplicates is for, and how one middleware runs both before and after another. The Kahn traversal emits every duplicate, so counting emitted definitions against the node map (which is keyed by unique name) reports a cycle where the graph has none, and Build turns that into an error the application panics on. */
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

/** @info The sentinel must still catch a real cycle: a depends-on-b and b depends-on-a. */
func TestOrderDefinitions_RealCycleIsDetected(t *testing.T) {
    first := NewHttpMiddlewareDefinition("a", 0, []string{"b"}, nil, nil, nil, nil, false, false)
    second := NewHttpMiddlewareDefinition("b", 0, []string{"a"}, nil, nil, nil, nil, false, false)

    _, _, cycleDetected := orderDefinitions([]*HttpMiddlewareDefinition{first, second})

    if false == cycleDetected {
        t.Fatalf("a mutual before-dependency is a cycle")
    }
}
