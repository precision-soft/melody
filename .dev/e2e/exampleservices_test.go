package main

import (
    "strings"
    "testing"
)

func exampleInventoryProbe() map[string]processServiceClassification {
    return map[string]processServiceClassification{
        "service.probe.stateless": {typeName: "*probe.Stateless", category: processServiceStateless},
        "service.probe.store":     {typeName: "*probe.Store", category: processServiceStore},
    }
}

func exampleDescriptionsProbe() []exampleContainerDescription {
    return []exampleContainerDescription{
        {Name: "service.probe.stateless", Lifetime: "container", TypeName: "*probe.Stateless"},
        {Name: "service.probe.store", Lifetime: "container", TypeName: "contract.Store"},
        {Name: "service.probe.trail", Lifetime: "scoped", TypeName: "*probe.Trail"},
    }
}

func exampleConcreteTypesProbe() map[string]string {
    return map[string]string{
        "service.probe.stateless": "*probe.Stateless",
        "service.probe.store":     "*probe.Store",
    }
}

/* the clean case pins the counts the passing line reports, so the three refusals below are refusals of THIS comparison and not of a fixture that was never clean */
func TestExampleProcessServiceProblems_AClassifiedRootHasNoProblem(t *testing.T) {
    problems, processServices, scopedServices := exampleProcessServiceProblems(exampleDescriptionsProbe(), exampleConcreteTypesProbe(), exampleInventoryProbe())

    if 0 != len(problems) {
        t.Fatalf("expected no problem, got %v", problems)
    }
    if 2 != processServices || 1 != scopedServices {
        t.Fatalf("expected 2 process services and 1 scoped, got %d and %d", processServices, scopedServices)
    }
}

func TestExampleProcessServiceProblems_AnUnclassifiedProcessServiceIsNamed(t *testing.T) {
    inventory := exampleInventoryProbe()
    delete(inventory, "service.probe.store")

    problems, _, _ := exampleProcessServiceProblems(exampleDescriptionsProbe(), exampleConcreteTypesProbe(), inventory)

    if 1 != len(problems) || false == strings.HasPrefix(problems[0], "service.probe.store: not classified") {
        t.Fatalf("expected the unclassified service to be named, got %v", problems)
    }
}

func TestExampleProcessServiceProblems_ATypeThatDriftedFromTheMeasuredOneIsNamed(t *testing.T) {
    concreteTypes := exampleConcreteTypesProbe()
    concreteTypes["service.probe.store"] = "*probe.StoreRewritten"

    problems, _, _ := exampleProcessServiceProblems(exampleDescriptionsProbe(), concreteTypes, exampleInventoryProbe())

    if 1 != len(problems) || false == strings.Contains(problems[0], "resolves to *probe.StoreRewritten where its state was measured on *probe.Store") {
        t.Fatalf("expected the drifted type to be named, got %v", problems)
    }
}

func TestExampleProcessServiceProblems_ADeadRowIsNamed(t *testing.T) {
    inventory := exampleInventoryProbe()
    inventory["service.probe.retired"] = processServiceClassification{typeName: "*probe.Retired", category: processServiceStateless}

    problems, _, _ := exampleProcessServiceProblems(exampleDescriptionsProbe(), exampleConcreteTypesProbe(), inventory)

    if 1 != len(problems) || false == strings.HasPrefix(problems[0], "service.probe.retired: classified but not registered") {
        t.Fatalf("expected the dead row to be named, got %v", problems)
    }
}

/* the build sweep answers a type only for a service it could build: a service the sweep did not reach and one whose build failed (an empty type) are both named, because a row nothing can be measured against is a row nobody can trust */
func TestExampleProcessServiceProblems_AServiceTheBuildSweepResolvedNoTypeForIsNamed(t *testing.T) {
    unbuilt := exampleConcreteTypesProbe()
    delete(unbuilt, "service.probe.store")

    problems, _, _ := exampleProcessServiceProblems(exampleDescriptionsProbe(), unbuilt, exampleInventoryProbe())
    if 1 != len(problems) || false == strings.HasPrefix(problems[0], "service.probe.store: the build sweep resolved no type") {
        t.Fatalf("expected the service the sweep did not reach to be named, got %v", problems)
    }

    failed := exampleConcreteTypesProbe()
    failed["service.probe.store"] = ""

    problems, _, _ = exampleProcessServiceProblems(exampleDescriptionsProbe(), failed, exampleInventoryProbe())
    if 1 != len(problems) || false == strings.HasPrefix(problems[0], "service.probe.store: the build sweep resolved no type") {
        t.Fatalf("expected the service whose build failed to be named, got %v", problems)
    }
}

/* two problems are reported in name order, so the band's output reads the same whatever order the maps were walked in */
func TestExampleProcessServiceProblems_ProblemsAreReportedInNameOrder(t *testing.T) {
    inventory := exampleInventoryProbe()
    delete(inventory, "service.probe.stateless")
    inventory["service.probe.retired"] = processServiceClassification{typeName: "*probe.Retired", category: processServiceStateless}

    problems, _, _ := exampleProcessServiceProblems(exampleDescriptionsProbe(), exampleConcreteTypesProbe(), inventory)
    if 2 != len(problems) || false == strings.HasPrefix(problems[0], "service.probe.retired:") || false == strings.HasPrefix(problems[1], "service.probe.stateless:") {
        t.Fatalf("expected the two problems in name order, got %v", problems)
    }
}

/* a scoped service is never asked for a row: its state is the request's by construction, and a row for it would be a dead row */
func TestExampleProcessServiceProblems_AScopedServiceNeedsNoRow(t *testing.T) {
    descriptions := []exampleContainerDescription{{Name: "service.probe.trail", Lifetime: "scoped", TypeName: "*probe.Trail"}}

    problems, processServices, scopedServices := exampleProcessServiceProblems(descriptions, map[string]string{}, map[string]processServiceClassification{})

    if 0 != len(problems) || 0 != processServices || 1 != scopedServices {
        t.Fatalf("expected the scoped service to be counted and left alone, got %v %d %d", problems, processServices, scopedServices)
    }
}
