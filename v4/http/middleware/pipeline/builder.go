package pipeline

import (
    "fmt"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v4/exception"
    exceptioncontract "github.com/precision-soft/melody/v4/exception/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
    kernelcontract "github.com/precision-soft/melody/v4/kernel/contract"
)

/* NewBuilder takes the definitions in order, copying each one's lists, so a caller's later write cannot reach a build; nil takes none. */
func NewBuilder(definitions []HttpMiddlewareDefinition) *Builder {
    builder := &Builder{}
    builder.Add(definitions)

    return builder
}

type Builder struct {
    definitions []*HttpMiddlewareDefinition
}

/* Add appends the definitions in order, copying each one's lists, since the builder reads them at every Build and Describe. */
func (instance *Builder) Add(definitions []HttpMiddlewareDefinition) {
    for _, definition := range definitions {
        taken := definition
        taken.Before = copyDefinitionList(definition.Before)
        taken.After = copyDefinitionList(definition.After)
        taken.Groups = copyDefinitionList(definition.Groups)
        taken.EnabledEnvironments = copyDefinitionList(definition.EnabledEnvironments)

        instance.definitions = append(instance.definitions, &taken)
    }
}

func (instance *Builder) Build(
    kernelInstance kernelcontract.Kernel,
    group string,
) ([]httpcontract.Middleware, *MiddlewareBuildReport, error) {
    ordered, report, selectionErr := instance.selectAndOrder(kernelInstance.Environment(), group)
    if nil != selectionErr {
        return nil, report, selectionErr
    }

    middlewares := make([]httpcontract.Middleware, 0, len(ordered))
    for _, definition := range ordered {
        if "" == definition.Name {
            continue
        }

        /* the report travels with the refusal, so a boot can explain why the pipeline could not be built */
        if nil == definition.Factory {
            return nil, report, exception.NewError(
                "middleware factory is nil",
                exceptioncontract.Context{
                    "middlewareName": definition.Name,
                },
                nil,
            )
        }

        middlewareValue, factoryErr := definition.Factory(kernelInstance)
        if nil != factoryErr {
            return nil, report, exception.NewError(
                "could not build middleware",
                exceptioncontract.Context{
                    "middlewareName": definition.Name,
                },
                factoryErr,
            )
        }

        /* a factory that succeeds and returns nothing is recorded as inactive with its reason */
        if nil == middlewareValue {
            report.SetInactive(
                append(
                    report.Inactive(),
                    NewInactiveMiddleware(definition.Name, "factory returned no middleware"),
                ),
            )

            continue
        }

        middlewares = append(middlewares, middlewareValue)
        report.SetSelectedNames(append(report.SelectedNames(), definition.Name))
    }

    return middlewares, report, nil
}

/* MiddlewareDescription is one pipeline entry as the selection and the ordering see it, with no factory run: the name, the priority and the function captured at registration, the middleware itself or its factory. */
type MiddlewareDescription struct {
    Name         string
    Priority     int
    FunctionName string
}

/* Describe answers what Build would run without running it: the same selection, gating, ordering and refusals, with no factory invoked; a definition whose factory answers a nil middleware, which Build skips, is listed here, since only the call reveals it. */
func (instance *Builder) Describe(
    environment string,
    group string,
) ([]MiddlewareDescription, *MiddlewareBuildReport, error) {
    ordered, report, selectionErr := instance.selectAndOrder(environment, group)
    if nil != selectionErr {
        return nil, report, selectionErr
    }

    descriptions := make([]MiddlewareDescription, 0, len(ordered))
    selectedNames := make([]string, 0, len(ordered))

    for _, definition := range ordered {
        if "" == definition.Name {
            continue
        }

        /* Build's nil-factory refusal is mirrored here, so a described pipeline is one the boot accepts */
        if nil == definition.Factory {
            return nil, report, exception.NewError(
                "middleware factory is nil",
                exceptioncontract.Context{
                    "middlewareName": definition.Name,
                },
                nil,
            )
        }

        descriptions = append(
            descriptions,
            MiddlewareDescription{
                Name:         definition.Name,
                Priority:     definition.Priority,
                FunctionName: definition.FunctionName,
            },
        )
        selectedNames = append(selectedNames, definition.Name)
    }

    report.SetSelectedNames(selectedNames)

    return descriptions, report, nil
}

/* selectAndOrder answers which definitions the environment and the group admit, and in what order, with Build's refusals. */
func (instance *Builder) selectAndOrder(
    environment string,
    group string,
) ([]*HttpMiddlewareDefinition, *MiddlewareBuildReport, error) {
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

    return ordered, report, nil
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
        if "" == definition.Name {
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
                    NewInactiveMiddleware(definition.Name, "disabled: environment mismatch"),
                ),
            )
            continue
        }

        if false == isEnabledForGroup(definition, group) {
            report.SetInactive(
                append(
                    report.Inactive(),
                    NewInactiveMiddleware(definition.Name, "disabled: group mismatch"),
                ),
            )
            continue
        }

        existingIndex, exists := seen[definition.Name]
        if true == exists && false == definition.AllowDuplicates {
            if true == definition.ReplaceExisting {
                selected[existingIndex] = definition
            } else {
                report.SetInactive(
                    append(
                        report.Inactive(),
                        NewInactiveMiddleware(definition.Name, "skipped: duplicate definition"),
                    ),
                )
            }
            continue
        }

        if false == exists {
            seen[definition.Name] = len(selected)
        }

        selected = append(selected, definition)
    }

    return selected
}

func isEnabledForEnvironment(definition *HttpMiddlewareDefinition, environment string) bool {
    if 0 == len(definition.EnabledEnvironments) {
        return true
    }

    for _, allowed := range definition.EnabledEnvironments {
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

    if 0 == len(definition.Groups) {
        return true
    }

    for _, g := range definition.Groups {
        if g == group {
            return true
        }
    }

    return false
}

/* validateReferenceGating refuses a before or after reference whose target is not active everywhere the referring definition is, so a pipeline that boots in dev cannot refuse to boot in prod. The declared environment sets are compared, never the booted environment; an empty set is universal. A name no definition carries is left to the ordering pass, which reports every missing reference together. */
func validateReferenceGating(definitions []*HttpMiddlewareDefinition, group string) error {
    if 0 == len(definitions) {
        return nil
    }

    /* only the definitions this build carries are weighed; another group's declarations cannot refuse this one */
    definitions = definitionsEnabledForGroup(definitions, group)
    if 0 == len(definitions) {
        return nil
    }

    byName := make(map[string][]*HttpMiddlewareDefinition)
    for _, definition := range definitions {
        if "" == definition.Name {
            continue
        }

        byName[definition.Name] = append(byName[definition.Name], definition)
    }

    for _, definition := range definitions {
        if "" == definition.Name {
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

            /* the reason is in the message, since an application boots on this error */
            return exception.NewError(
                fmt.Sprintf("middleware pipeline has an unsatisfiable reference: %s", reason),
                exceptioncontract.Context{
                    "group":      group,
                    "middleware": definition.Name,
                    "references": referencedName,
                    "reason":     reason,
                },
                nil,
            )
        }
    }

    return nil
}

/* definitionsEnabledForGroup keeps the definitions the named group assembles; the environment is deliberately not applied. */
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

/* supportedEnvironments lists every value config.validateEnvironment admits, copied to keep the pipeline free of the config package; TestSupportedEnvironments_MatchesTheConfigurationPackage fails if they diverge. */
var supportedEnvironments = []string{"dev", "prod"}

/* referencedNames lists the definitions this one orders itself against, in edge order, so the first unsatisfiable reference reported is stable. */
func referencedNames(definition *HttpMiddlewareDefinition) []string {
    names := make([]string, 0, len(definition.After)+len(definition.Before))

    for _, afterName := range definition.After {
        if "" == afterName {
            continue
        }

        names = append(names, afterName)
    }

    for _, beforeName := range definition.Before {
        if "" == beforeName {
            continue
        }

        names = append(names, beforeName)
    }

    return names
}

/* gatingReason explains why no registration under the referenced name is active everywhere the referrer is, and answers "" when one is or when their union covers it. Only the environment gating is weighed; a group is a property of the build, whose selection already dropped what it does not carry. */
func gatingReason(referrer *HttpMiddlewareDefinition, targets []*HttpMiddlewareDefinition) string {
    for _, target := range targets {
        if true == coversDeclaredSet(target.EnabledEnvironments, referrer.EnabledEnvironments) {
            return ""
        }
    }

    /* the edge orders against the name, so the union of its registrations may cover the referrer, as an `auth` for dev beside an `auth` for prod does */
    if true == coversDeclaredSet(unionOfDeclaredSets(targets), referrer.EnabledEnvironments) {
        return ""
    }

    target := targets[0]

    return describeReference(
        referrer.Name,
        target.Name,
        "environment",
        referrer.EnabledEnvironments,
        target.EnabledEnvironments,
    )
}

/* unionOfDeclaredSets merges the environment sets registered under one name and answers the universal empty slice when the merge names every environment config.validateEnvironment admits. */
func unionOfDeclaredSets(definitions []*HttpMiddlewareDefinition) []string {
    merged := make([]string, 0, len(definitions))
    seen := make(map[string]bool, len(definitions))

    for _, definition := range definitions {
        if 0 == len(definition.EnabledEnvironments) {
            return nil
        }

        for _, environment := range definition.EnabledEnvironments {
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

/* coversDeclaredSet reports whether the declared set admits everything the other admits; an empty slice is universal. */
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
    /* duplicates share a name and therefore a node, so every one is kept and emitted together at the node's position */
    definition *HttpMiddlewareDefinition
    duplicates []*HttpMiddlewareDefinition
    inDegree   int
    out        []string
    /* registration rank, so equal-priority definitions keep registration order; the generated names would sort 1, 10, 2 */
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
        if "" == definition.Name {
            continue
        }

        if existingNode, exists := nodes[definition.Name]; true == exists {
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

        nodes[definition.Name] = node
        orderedNodes = append(orderedNodes, node)
    }

    /* at most one end of an edge can be missing, and it is always the name the definition referenced */
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
        if "" == definition.Name {
            continue
        }

        for _, afterName := range definition.After {
            if "" == afterName {
                continue
            }
            addEdge(afterName, definition.Name)
        }

        for _, beforeName := range definition.Before {
            if "" == beforeName {
                continue
            }
            addEdge(definition.Name, beforeName)
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
            leftPriority := ready[left].definition.Priority
            rightPriority := ready[right].definition.Priority

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
    /* count the nodes drained, not the definitions emitted, since a node carries every duplicate of its name */
    if processedNodes != len(nodes) {
        cycleDetected = true

        result = make([]*HttpMiddlewareDefinition, 0, len(definitions))
        result = append(result, definitions...)

        sort.SliceStable(result, func(left int, right int) bool {
            return result[left].Priority < result[right].Priority
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
