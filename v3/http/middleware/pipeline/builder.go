package pipeline

import (
    "fmt"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    httpcontract "github.com/precision-soft/melody/v3/http/contract"
    kernelcontract "github.com/precision-soft/melody/v3/kernel/contract"
)

func NewBuilder(definitions ...*HttpMiddlewareDefinition) *Builder {
    return &Builder{
        definitions: definitions,
    }
}

type Builder struct {
    definitions []*HttpMiddlewareDefinition
}

func (instance *Builder) Add(definitions ...*HttpMiddlewareDefinition) {
    if 0 == len(definitions) {
        return
    }

    instance.definitions = append(instance.definitions, definitions...)
}

func (instance *Builder) Build(
    kernelInstance kernelcontract.Kernel,
    group string,
) ([]httpcontract.Middleware, *MiddlewareBuildReport, error) {
    environment := kernelInstance.Environment()

    report := &MiddlewareBuildReport{
        requestedGroup: group,
        kernelEnv:      environment,
        selectedNames:  make([]string, 0),
        inactive:       make([]*InactiveMiddleware, 0),
    }

    selected := instance.selectDefinitions(environment, group, report)

    gatingErr := validateReferenceGating(instance.definitions, group)
    if nil != gatingErr {
        return nil, report, gatingErr
    }

    ordered, missingReferences, cycleDetected := orderDefinitions(selected)
    report.SetMissingReference(missingReferences)
    report.SetCycleDetected(cycleDetected)

    if true == cycleDetected {
        return nil, report, exception.NewError(
            "middleware pipeline has a cycle",
            exceptioncontract.Context{
                "group": group,
            },
            nil,
        )
    }

    if 0 < len(missingReferences) {
        return nil, report, exception.NewError(
            "middleware pipeline has missing references",
            exceptioncontract.Context{
                "group":             group,
                "missingReferences": missingReferences,
            },
            nil,
        )
    }

    middlewares := make([]httpcontract.Middleware, 0, len(ordered))
    for _, definition := range ordered {
        if "" == definition.name {
            continue
        }

        if nil == definition.factory {
            return nil, nil, exception.NewError(
                "middleware factory is nil",
                exceptioncontract.Context{
                    "middlewareName": definition.name,
                },
                nil,
            )
        }

        middlewareValue, factoryErr := definition.factory(kernelInstance)
        if nil != factoryErr {
            return nil, nil, exception.NewError(
                "could not build middleware",
                exceptioncontract.Context{
                    "middlewareName": definition.name,
                },
                factoryErr,
            )
        }

        if nil == middlewareValue {
            continue
        }

        middlewares = append(middlewares, middlewareValue)
        report.SetSelectedNames(append(report.SelectedNames(), definition.name))
    }

    return middlewares, report, nil
}

func (instance *Builder) selectDefinitions(
    environment string,
    group string,
    report *MiddlewareBuildReport,
) []*HttpMiddlewareDefinition {
    if 0 == len(instance.definitions) {
        return []*HttpMiddlewareDefinition{}
    }

    selected := make([]*HttpMiddlewareDefinition, 0, len(instance.definitions))
    seen := make(map[string]int)

    for _, definition := range instance.definitions {
        if "" == definition.name {
            report.SetInactive(
                append(
                    report.Inactive(),
                    NewInactiveMiddleware("", "skipped: empty name"),
                ),
            )
            continue
        }

        if false == isEnabledForEnvironment(definition, environment) {
            report.SetInactive(
                append(
                    report.Inactive(),
                    NewInactiveMiddleware(definition.name, "disabled: environment mismatch"),
                ),
            )
            continue
        }

        if false == isEnabledForGroup(definition, group) {
            report.SetInactive(
                append(
                    report.Inactive(),
                    NewInactiveMiddleware(definition.name, "disabled: group mismatch"),
                ),
            )
            continue
        }

        existingIndex, exists := seen[definition.name]
        if true == exists && false == definition.allowDuplicates {
            if true == definition.replaceExisting {
                selected[existingIndex] = definition
            } else {
                report.SetInactive(
                    append(
                        report.Inactive(),
                        NewInactiveMiddleware(definition.name, "skipped: duplicate definition"),
                    ),
                )
            }
            continue
        }

        if false == exists {
            seen[definition.name] = len(selected)
        }

        selected = append(selected, definition)
    }

    return selected
}

func isEnabledForEnvironment(definition *HttpMiddlewareDefinition, environment string) bool {
    if 0 == len(definition.enabledEnvironments) {
        return true
    }

    for _, allowed := range definition.enabledEnvironments {
        if allowed == environment {
            return true
        }
    }

    return false
}

func isEnabledForGroup(definition *HttpMiddlewareDefinition, group string) bool {
    if "" == strings.TrimSpace(group) {
        return true
    }

    if 0 == len(definition.groups) {
        return true
    }

    for _, g := range definition.groups {
        if g == group {
            return true
        }
    }

    return false
}

/* validateReferenceGating refuses a before or after reference whose target is not active everywhere the referring definition is active. selectDefinitions drops a definition whose environment or group gating does not match; orderDefinitions then sees the surviving reference as a name no node carries and reports it as missing, and Build turns that into the error the application boots on. A pipeline where an always-on middleware orders itself against a dev-only one therefore starts in dev and refuses to start in prod, which is the one place the failure must not be discovered.

The declared sets are compared, never the environment being booted, so the same reference is refused in every environment rather than only in the one that happens to drop the target. An empty set is the universal one — a definition that names no environments runs in all of them — so an always-on definition may only reference another always-on definition, while a dev-only definition may reference a dev-only or an always-on one. Groups carry the same meaning and are checked the same way.

A name no definition carries at all is left alone: that is an ordinary missing reference, and the ordering pass reports every one of them together. */
func validateReferenceGating(definitions []*HttpMiddlewareDefinition, group string) error {
    if 0 == len(definitions) {
        return nil
    }

    /* only what this build carries is weighed, which is what the paragraph above says and what the pass did not do: it was handed every definition the builder holds, so building the `web` group reported an unsatisfiable reference between two definitions confined to `api` — a pair `web` never assembles and whose gating says nothing about it. Several groups are built in one process, and a group that refuses to build because of another group's declarations refuses for a reason no request to it could ever reach. */
    definitions = definitionsEnabledForGroup(definitions, group)
    if 0 == len(definitions) {
        return nil
    }

    byName := make(map[string][]*HttpMiddlewareDefinition)
    for _, definition := range definitions {
        if "" == definition.name {
            continue
        }

        byName[definition.name] = append(byName[definition.name], definition)
    }

    for _, definition := range definitions {
        if "" == definition.name {
            continue
        }

        for _, referencedName := range referencedNames(definition) {
            targets, exists := byName[referencedName]
            if false == exists {
                continue
            }

            reason := gatingReason(definition, targets)
            if "" == reason {
                continue
            }

            /* the reason belongs in the message, not only in the context: an application boots on this error and the operator reading the panic has to be told which two definitions and which gating are at fault without unwrapping anything */
            return exception.NewError(
                fmt.Sprintf("middleware pipeline has an unsatisfiable reference: %s", reason),
                exceptioncontract.Context{
                    "group":      group,
                    "middleware": definition.name,
                    "references": referencedName,
                    "reason":     reason,
                },
                nil,
            )
        }
    }

    return nil
}

/* definitionsEnabledForGroup keeps the definitions the named group assembles. The environment is deliberately not applied: an environment is a property of the running process and the whole point of the check is to settle, at every boot, a reference that would survive one environment and vanish in another. */
func definitionsEnabledForGroup(definitions []*HttpMiddlewareDefinition, group string) []*HttpMiddlewareDefinition {
    enabled := make([]*HttpMiddlewareDefinition, 0, len(definitions))

    for _, definition := range definitions {
        if false == isEnabledForGroup(definition, group) {
            continue
        }

        enabled = append(enabled, definition)
    }

    return enabled
}

/* supportedEnvironments lists every value config.validateEnvironment admits. A name registered across all of them is registered everywhere, which is what lets the union of same-named definitions answer a reference from an always-on one. It is a copy, deliberately: importing config here would tie the middleware pipeline to the configuration package for one slice. TestSupportedEnvironments_MatchesTheConfigurationPackage fails if the two ever diverge. */
var supportedEnvironments = []string{"dev", "prod"}

/* referencedNames lists the definitions this one orders itself against, in the order the edges are added, so the first unsatisfiable reference reported is stable across builds. */
func referencedNames(definition *HttpMiddlewareDefinition) []string {
    names := make([]string, 0, len(definition.after)+len(definition.before))

    for _, afterName := range definition.after {
        if "" == afterName {
            continue
        }

        names = append(names, afterName)
    }

    for _, beforeName := range definition.before {
        if "" == beforeName {
            continue
        }

        names = append(names, beforeName)
    }

    return names
}

/* gatingReason explains why none of the definitions registered under the referenced name is active everywhere the referrer is, and returns an empty string when one of them is. Duplicates registered under one name are each a candidate: a single one that covers the referrer is enough, because that one is selected wherever the referrer is. */
/* gatingReason weighs the environment gating alone. An environment is a property of the running process, so a reference that survives one environment and vanishes in another is a defect the declared sets can settle once, at every boot. A group is a property of the build being asked for: several groups are built in one process, each from its own selection, so a reference unsatisfiable in some other group says nothing about this one — the selection has already dropped what this group does not carry, and a target missing from it is reported as a missing reference like any other. */
func gatingReason(referrer *HttpMiddlewareDefinition, targets []*HttpMiddlewareDefinition) string {
    for _, target := range targets {
        if true == coversDeclaredSet(target.enabledEnvironments, referrer.enabledEnvironments) {
            return ""
        }
    }

    /* no single registration covers the referrer, but the referenced NAME is what the edge orders against and every registration under it answers to that name: what has to be present wherever the referrer runs is one of them, not one particular one. Splitting a middleware across environments is the ordinary way to write it — an `auth` for development beside an `auth` for production — and requiring either alone to cover an always-on referrer refuses a configuration that boots correctly in both. */
    if true == coversDeclaredSet(unionOfDeclaredSets(targets), referrer.enabledEnvironments) {
        return ""
    }

    target := targets[0]

    return describeReference(
        referrer.name,
        target.name,
        "environment",
        referrer.enabledEnvironments,
        target.enabledEnvironments,
    )
}

/* unionOfDeclaredSets merges the environment sets of every registration under one name, and reports the merge as universal — the empty slice the rest of this file reads as "everywhere" — when it names every environment a melody process is allowed to run in.

That last step is what makes the union answer an always-on referrer at all. The environment is not free-form: config.validateEnvironment refuses to boot on anything that is not `dev` or `prod`, so a name registered for both is registered for every environment that can exist, and treating the merge as merely `{dev, prod}` would refuse a configuration no process can ever fall outside of. The list is duplicated here rather than imported to keep the pipeline free of a dependency on config; supportedEnvironments carries the guard against the two drifting apart. */
func unionOfDeclaredSets(definitions []*HttpMiddlewareDefinition) []string {
    merged := make([]string, 0, len(definitions))
    seen := make(map[string]bool, len(definitions))

    for _, definition := range definitions {
        if 0 == len(definition.enabledEnvironments) {
            return nil
        }

        for _, environment := range definition.enabledEnvironments {
            if true == seen[environment] {
                continue
            }

            seen[environment] = true
            merged = append(merged, environment)
        }
    }

    for _, environment := range supportedEnvironments {
        if false == seen[environment] {
            return merged
        }
    }

    return nil
}

/* coversDeclaredSet reports whether the declared set admits at least everything the other one admits. An empty slice is the universal set, so it covers anything and is covered only by another universal set. */
func coversDeclaredSet(superset []string, subset []string) bool {
    if 0 == len(superset) {
        return true
    }

    if 0 == len(subset) {
        return false
    }

    for _, value := range subset {
        found := false

        for _, allowed := range superset {
            if allowed == value {
                found = true
                break
            }
        }

        if false == found {
            return false
        }
    }

    return true
}

func describeReference(
    referrerName string,
    targetName string,
    dimension string,
    referrerSet []string,
    targetSet []string,
) string {
    return fmt.Sprintf(
        "%q is enabled in %s and %q only in %s, so the reference is dropped wherever %q is not enabled",
        referrerName,
        describeDeclaredSet(dimension, referrerSet),
        targetName,
        describeDeclaredSet(dimension, targetSet),
        targetName,
    )
}

func describeDeclaredSet(dimension string, values []string) string {
    if 0 == len(values) {
        return fmt.Sprintf("every %s", dimension)
    }

    return fmt.Sprintf("%s %s", dimension, strings.Join(values, ", "))
}

type definitionNode struct {
    /* definition drives ordering (priority, before/after edges); duplicates share a name and therefore a node, so every one of them is kept here and emitted together at the node's position — keying the map on the name alone silently dropped all but the last, defeating allowDuplicates */
    definition *HttpMiddlewareDefinition
    duplicates []*HttpMiddlewareDefinition
    inDegree   int
    out        []string
    /* registration rank, so equal-priority definitions keep the order the application registered them in; the generated names carry that order as a decimal counter, which a lexicographic tie-break reads as 1, 10, 11, 2 and which sorts every factory ahead of every middleware */
    order int
}

func orderDefinitions(definitions []*HttpMiddlewareDefinition) ([]*HttpMiddlewareDefinition, []string, bool) {
    if 0 == len(definitions) {
        return []*HttpMiddlewareDefinition{}, []string{}, false
    }

    nodes := make(map[string]*definitionNode)
    orderedNodes := make([]*definitionNode, 0, len(definitions))
    missingReferences := make([]string, 0)

    for _, definition := range definitions {
        if "" == definition.name {
            continue
        }

        if existingNode, exists := nodes[definition.name]; true == exists {
            existingNode.duplicates = append(existingNode.duplicates, definition)
            continue
        }

        node := &definitionNode{
            definition: definition,
            duplicates: []*HttpMiddlewareDefinition{definition},
            inDegree:   0,
            out:        make([]string, 0),
            order:      len(orderedNodes),
        }

        nodes[definition.name] = node
        orderedNodes = append(orderedNodes, node)
    }

    /* every edge has the iterated definition on one of its two ends, and that end was given a node above, so at most one end can be missing and the missing one is always the name the definition referenced */
    addEdge := func(from string, to string) {
        fromNode, fromExists := nodes[from]
        toNode, toExists := nodes[to]

        if false == toExists {
            missingReferences = append(missingReferences, to)
            return
        }

        if false == fromExists {
            missingReferences = append(missingReferences, from)
            return
        }

        fromNode.out = append(fromNode.out, to)
        toNode.inDegree = toNode.inDegree + 1
    }

    for _, definition := range definitions {
        if "" == definition.name {
            continue
        }

        for _, afterName := range definition.after {
            if "" == afterName {
                continue
            }
            addEdge(afterName, definition.name)
        }

        for _, beforeName := range definition.before {
            if "" == beforeName {
                continue
            }
            addEdge(definition.name, beforeName)
        }
    }

    ready := make([]*definitionNode, 0)
    for _, node := range orderedNodes {
        if 0 == node.inDegree {
            ready = append(ready, node)
        }
    }

    sortReady := func() {
        sort.SliceStable(ready, func(left int, right int) bool {
            leftPriority := ready[left].definition.priority
            rightPriority := ready[right].definition.priority

            if leftPriority != rightPriority {
                return leftPriority < rightPriority
            }

            return ready[left].order < ready[right].order
        })
    }

    sortReady()

    result := make([]*HttpMiddlewareDefinition, 0, len(definitions))
    processedNodes := 0

    for 0 < len(ready) {
        node := ready[0]
        ready = ready[1:]
        processedNodes++

        result = append(result, node.duplicates...)

        for _, toName := range node.out {
            toNode := nodes[toName]
            if nil == toNode {
                continue
            }

            toNode.inDegree = toNode.inDegree - 1
            if 0 == toNode.inDegree {
                ready = append(ready, toNode)
            }
        }

        sortReady()
    }

    cycleDetected := false
    /* count the NODES drained, not the definitions emitted: a node carries every duplicate of its name, so comparing the emitted definitions against the name-keyed node map reports a cycle for the duplicates allowDuplicates exists to permit */
    if processedNodes != len(nodes) {
        cycleDetected = true

        result = make([]*HttpMiddlewareDefinition, 0, len(definitions))
        result = append(result, definitions...)

        sort.SliceStable(result, func(left int, right int) bool {
            return result[left].priority < result[right].priority
        })
    }

    missingReferences = uniqueSorted(missingReferences)

    return result, missingReferences, cycleDetected
}

func uniqueSorted(values []string) []string {
    if 0 == len(values) {
        return []string{}
    }

    unique := make(map[string]struct{})
    for _, v := range values {
        if "" == v {
            continue
        }
        unique[v] = struct{}{}
    }

    result := make([]string, 0, len(unique))
    for k := range unique {
        result = append(result, k)
    }

    sort.Strings(result)

    return result
}
