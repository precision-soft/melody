package container

import (
    "container/heap"
    "reflect"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v3/exception"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
    "github.com/precision-soft/melody/v3/internal"
)

func (instance *container) Close() error {
    type closer interface {
        Close() error
    }

    type closeCandidate struct {
        nodeKey string
        value   any
    }

    instance.mutex.Lock()

    if true == instance.isClosed {
        existingErr := instance.closeErr
        instance.mutex.Unlock()
        return existingErr
    }

    typeStringToType := make(map[string]reflect.Type, len(instance.typeInstances))

    createdNodeKeys := make(
        []string,
        0,
        len(instance.instances)+len(instance.typeInstances),
    )

    for serviceName := range instance.instances {
        createdNodeKeys = append(createdNodeKeys, "service:"+serviceName)
    }

    for targetType := range instance.typeInstances {
        typeString := targetType.String()
        typeStringToType[typeString] = targetType
        createdNodeKeys = append(createdNodeKeys, "type:"+typeString)
    }

    sort.Slice(
        createdNodeKeys,
        func(leftIndex int, rightIndex int) bool {
            return createdNodeKeys[leftIndex] > createdNodeKeys[rightIndex]
        },
    )

    resolveNodeValue := func(nodeKey string) (any, bool) {
        if true == strings.HasPrefix(nodeKey, "service:") {
            serviceName := strings.TrimPrefix(nodeKey, "service:")
            instanceValue, exists := instance.instances[serviceName]
            return instanceValue, exists
        }

        if true == strings.HasPrefix(nodeKey, "type:") {
            typeString := strings.TrimPrefix(nodeKey, "type:")
            targetType, typeExists := typeStringToType[typeString]
            if false == typeExists {
                return nil, false
            }

            instanceValue, exists := instance.typeInstances[targetType]
            return instanceValue, exists
        }

        return nil, false
    }

    /** @important the same instance can be created under several node keys (a named service that also registers its type lives under both "service:<name>" and "type:<T>"); collapse those aliases onto one representative so a dependency edge recorded against any alias constrains the close order of the shared instance and it is closed exactly once in dependent-before-dependency order */
    valueOfNodeKey := make(map[string]any, len(createdNodeKeys))
    representativeOf := make(map[string]string, len(createdNodeKeys))
    pointerRepresentative := make(map[uintptr]string, len(createdNodeKeys))
    valueRepresentative := make(map[any]string, len(createdNodeKeys))
    canonicalNodeKeys := make([]string, 0, len(createdNodeKeys))

    for _, nodeKey := range createdNodeKeys {
        value, exists := resolveNodeValue(nodeKey)
        if false == exists {
            continue
        }

        valueOfNodeKey[nodeKey] = value

        if pointerKey, hasPointer := pointerKeyOf(value); true == hasPointer {
            if existingRepresentative, alreadyGrouped := pointerRepresentative[pointerKey]; true == alreadyGrouped {
                representativeOf[nodeKey] = existingRepresentative

                continue
            }

            pointerRepresentative[pointerKey] = nodeKey
            representativeOf[nodeKey] = nodeKey
            canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)

            continue
        }

        if true == isComparableValue(value) {
            if existingRepresentative, alreadyGrouped := valueRepresentative[value]; true == alreadyGrouped {
                representativeOf[nodeKey] = existingRepresentative

                continue
            }

            valueRepresentative[value] = nodeKey
            representativeOf[nodeKey] = nodeKey
            canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)

            continue
        }

        representativeOf[nodeKey] = nodeKey
        canonicalNodeKeys = append(canonicalNodeKeys, nodeKey)
    }

    adjacency := make(map[string]map[string]struct{}, len(canonicalNodeKeys))
    inDegree := make(map[string]int, len(canonicalNodeKeys))

    for _, nodeKey := range canonicalNodeKeys {
        inDegree[nodeKey] = 0
    }

    for dependentKey, dependencySet := range instance.dependencyGraph {
        canonicalDependent, dependentCreated := representativeOf[dependentKey]
        if false == dependentCreated {
            continue
        }

        for dependencyKey := range dependencySet {
            canonicalDependency, dependencyCreated := representativeOf[dependencyKey]
            if false == dependencyCreated {
                continue
            }

            if canonicalDependent == canonicalDependency {
                continue
            }

            dependencies, exists := adjacency[canonicalDependent]
            if false == exists {
                dependencies = make(map[string]struct{})
                adjacency[canonicalDependent] = dependencies
            }

            if _, alreadyAdded := dependencies[canonicalDependency]; true == alreadyAdded {
                continue
            }

            dependencies[canonicalDependency] = struct{}{}
            inDegree[canonicalDependency] = inDegree[canonicalDependency] + 1
        }
    }

    available := make([]string, 0, len(createdNodeKeys))
    for nodeKey, degree := range inDegree {
        if 0 == degree {
            available = append(available, nodeKey)
        }
    }

    availableHeap := &nodeKeyHeap{
        items: available,
    }
    heap.Init(availableHeap)

    closeOrder := make([]string, 0, len(createdNodeKeys))

    for 0 < availableHeap.Len() {
        current := heap.Pop(availableHeap).(string)

        closeOrder = append(closeOrder, current)

        dependencies, exists := adjacency[current]
        if false == exists {
            continue
        }

        for dependencyKey := range dependencies {
            inDegree[dependencyKey] = inDegree[dependencyKey] - 1
            if 0 == inDegree[dependencyKey] {
                heap.Push(
                    availableHeap,
                    dependencyKey,
                )
            }
        }
    }

    remaining := make([]string, 0)
    for nodeKey, degree := range inDegree {
        if 0 < degree {
            remaining = append(remaining, nodeKey)
        }
    }

    sort.Slice(
        remaining,
        func(leftIndex int, rightIndex int) bool {
            return remaining[leftIndex] > remaining[rightIndex]
        },
    )

    dependencyCycleDetected := false
    if 0 < len(remaining) {
        dependencyCycleDetected = true
        for _, nodeKey := range remaining {
            closeOrder = append(closeOrder, nodeKey)
        }
    }

    candidates := make([]closeCandidate, 0, len(closeOrder))

    for _, nodeKey := range closeOrder {
        value, exists := valueOfNodeKey[nodeKey]
        if false == exists {
            continue
        }

        candidates = append(
            candidates,
            closeCandidate{
                nodeKey: nodeKey,
                value:   value,
            },
        )
    }

    instance.mutex.Unlock()

    closedPointers := make(map[uintptr]struct{})
    closedValues := make(map[any]struct{})
    failures := make(map[string]string)

    for _, candidate := range candidates {
        pointerKey, hasPointer := pointerKeyOf(candidate.value)
        comparableValue := false == hasPointer && true == isComparableValue(candidate.value)

        if true == hasPointer {
            if _, alreadyClosed := closedPointers[pointerKey]; true == alreadyClosed {
                continue
            }
        } else if true == comparableValue {
            if _, alreadyClosed := closedValues[candidate.value]; true == alreadyClosed {
                continue
            }
        }

        closeable, isCloseable := candidate.value.(closer)
        if false == isCloseable {
            if true == hasPointer {
                closedPointers[pointerKey] = struct{}{}
            } else if true == comparableValue {
                closedValues[candidate.value] = struct{}{}
            }

            continue
        }

        closeErr := closeable.Close()
        if nil != closeErr {
            failures[candidate.nodeKey] = closeErr.Error()
        }

        if true == hasPointer {
            closedPointers[pointerKey] = struct{}{}
        } else if true == comparableValue {
            closedValues[candidate.value] = struct{}{}
        }
    }

    var resultErr error

    if true == dependencyCycleDetected {
        if 0 == len(failures) {
            resultErr = exception.NewError(
                "container close dependency cycle detected",
                exceptioncontract.Context{
                    "nodes": remaining,
                },
                nil,
            )
        } else {
            failures["container.dependencyCycle"] = "dependency cycle detected"
        }
    }

    if nil == resultErr && 0 < len(failures) {
        resultErr = exception.NewError(
            "failed to close container services",
            exceptioncontract.Context{
                "failures": failures,
            },
            nil,
        )
    }

    instance.mutex.Lock()
    instance.isClosed = true
    instance.closeErr = resultErr
    instance.mutex.Unlock()

    return resultErr
}

type nodeKeyHeap struct {
    items []string
}

func (instance *nodeKeyHeap) Len() int {
    return len(instance.items)
}

func (instance *nodeKeyHeap) Less(leftIndex int, rightIndex int) bool {
    return instance.items[leftIndex] > instance.items[rightIndex]
}

func (instance *nodeKeyHeap) Swap(leftIndex int, rightIndex int) {
    instance.items[leftIndex], instance.items[rightIndex] = instance.items[rightIndex], instance.items[leftIndex]
}

func (instance *nodeKeyHeap) Push(value any) {
    instance.items = append(instance.items, value.(string))
}

func (instance *nodeKeyHeap) Pop() any {
    lastIndex := len(instance.items) - 1
    value := instance.items[lastIndex]
    instance.items = instance.items[:lastIndex]
    return value
}

func isComparableValue(value any) bool {
    if nil == value {
        return false
    }

    return reflect.TypeOf(value).Comparable()
}

func pointerKeyOf(value any) (uintptr, bool) {
    if true == internal.IsNilInterface(value) {
        return 0, false
    }

    reflected := reflect.ValueOf(value)

    for reflect.Interface == reflected.Kind() {
        reflected = reflected.Elem()
    }

    if reflect.Pointer == reflected.Kind() && false == reflected.IsNil() {
        return reflected.Pointer(), true
    }

    return 0, false
}
