package pipeline

import (
    "sort"
    "strings"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    httpcontract "github.com/precision-soft/melody/v2/http/contract"
    kernelcontract "github.com/precision-soft/melody/v2/kernel/contract"
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

type definitionNode struct {
    definition *HttpMiddlewareDefinition
    inDegree   int
    out        []string
}

func orderDefinitions(definitions []*HttpMiddlewareDefinition) ([]*HttpMiddlewareDefinition, []string, bool) {
    if 0 == len(definitions) {
        return []*HttpMiddlewareDefinition{}, []string{}, false
    }

    nodes := make(map[string]*definitionNode)
    missingReferences := make([]string, 0)

    for _, definition := range definitions {
        if "" == definition.name {
            continue
        }

        nodes[definition.name] = &definitionNode{
            definition: definition,
            inDegree:   0,
            out:        make([]string, 0),
        }
    }

    addEdge := func(from string, to string) {
        fromNode, fromExists := nodes[from]
        toNode, toExists := nodes[to]

        if false == fromExists || false == toExists {
            if true == fromExists && false == toExists {
                missingReferences = append(missingReferences, to)
            }
            if false == fromExists && true == toExists {
                missingReferences = append(missingReferences, from)
            }
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
    for _, node := range nodes {
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

            return ready[left].definition.name < ready[right].definition.name
        })
    }

    sortReady()

    result := make([]*HttpMiddlewareDefinition, 0, len(nodes))

    for 0 < len(ready) {
        node := ready[0]
        ready = ready[1:]

        result = append(result, node.definition)

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
    if len(result) != len(nodes) {
        cycleDetected = true

        result = make([]*HttpMiddlewareDefinition, 0, len(definitions))
        result = append(result, definitions...)

        sort.SliceStable(result, func(left int, right int) bool {
            if result[left].priority != result[right].priority {
                return result[left].priority < result[right].priority
            }
            return result[left].name < result[right].name
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
