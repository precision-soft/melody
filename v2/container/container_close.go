package container

import (
    "container/heap"
    "reflect"
    "sort"
    "strings"

    "github.com/precision-soft/melody/v2/exception"
    exceptioncontract "github.com/precision-soft/melody/v2/exception/contract"
    "github.com/precision-soft/melody/v2/internal"
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

    createdSet := make(map[string]struct{}, len(createdNodeKeys))
    for _, nodeKey := range createdNodeKeys {
        createdSet[nodeKey] = struct{}{}
    }

    adjacency := make(map[string]map[string]struct{}, len(createdNodeKeys))
    inDegree := make(map[string]int, len(createdNodeKeys))

    for _, nodeKey := range createdNodeKeys {
        inDegree[nodeKey] = 0
    }

    for dependentKey, dependencySet := range instance.dependencyGraph {
        if _, dependentCreated := createdSet[dependentKey]; false == dependentCreated {
            continue
        }

        for dependencyKey := range dependencySet {
            if _, dependencyCreated := createdSet[dependencyKey]; false == dependencyCreated {
                continue
            }

            dependencies, exists := adjacency[dependentKey]
            if false == exists {
                dependencies = make(map[string]struct{})
                adjacency[dependentKey] = dependencies
            }

            if _, alreadyAdded := dependencies[dependencyKey]; true == alreadyAdded {
                continue
            }

            dependencies[dependencyKey] = struct{}{}
            inDegree[dependencyKey] = inDegree[dependencyKey] + 1
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
        value := any(nil)

        if true == strings.HasPrefix(nodeKey, "service:") {
            serviceName := strings.TrimPrefix(nodeKey, "service:")
            instanceValue, exists := instance.instances[serviceName]
            if false == exists {
                continue
            }

            value = instanceValue
        }

        if true == strings.HasPrefix(nodeKey, "type:") {
            typeString := strings.TrimPrefix(nodeKey, "type:")
            targetType, exists := typeStringToType[typeString]
            if false == exists {
                continue
            }

            instanceValue, exists := instance.typeInstances[targetType]
            if false == exists {
                continue
            }

            value = instanceValue
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
    failures := make(map[string]string)

    for _, candidate := range candidates {
        pointerKey, hasPointer := pointerKeyOf(candidate.value)
        if true == hasPointer {
            if _, alreadyClosed := closedPointers[pointerKey]; true == alreadyClosed {
                continue
            }
        }

        closeable, isCloseable := candidate.value.(closer)
        if false == isCloseable {
            if true == hasPointer {
                closedPointers[pointerKey] = struct{}{}
            }

            continue
        }

        closeErr := closeable.Close()
        if nil != closeErr {
            failures[candidate.nodeKey] = closeErr.Error()
        }

        if true == hasPointer {
            closedPointers[pointerKey] = struct{}{}
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
