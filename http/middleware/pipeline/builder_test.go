package pipeline

import (
    "fmt"
    "testing"
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
