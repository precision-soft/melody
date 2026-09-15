package container

import (
    "context"
    "sort"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* TeardownPlan computes a read-only snapshot under the container read lock. Inferred and expanded edges belong only to that snapshot. After Close it still lists built services but omits released inferred edges; it is not a retained record of the executed plan. */
func (instance *container) TeardownPlan() []containercontract.TeardownPlanEntry {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    plan := instance.teardownPlanLocked()

    entries := make([]containercontract.TeardownPlanEntry, 0, len(plan.closeOrder))

    cycleMembers := make(map[string]struct{}, len(plan.cycleNodeKeys))
    for _, cycleNodeKey := range plan.cycleNodeKeys {
        cycleMembers[cycleNodeKey] = struct{}{}
    }

    for _, nodeKey := range plan.closeOrder {
        dependencies := make([]string, 0, len(plan.canonicalEdges[nodeKey]))

        for dependencyKey := range plan.canonicalEdges[nodeKey] {
            dependencies = append(dependencies, dependencyKey)
        }

        sort.Strings(dependencies)

        _, inCycle := cycleMembers[nodeKey]

        entries = append(
            entries,
            containercontract.TeardownPlanEntry{
                NodeKey:      nodeKey,
                WaveIndex:    plan.closeWaveIndexOf[nodeKey],
                Dependencies: dependencies,
                SerialGroup:  plan.unorderedGroupOf[nodeKey],
                Aliases:      append([]string(nil), plan.aliasesOf[nodeKey]...),
                Cycle:        inCycle,
            },
        )
    }

    return entries
}

/* TeardownRunsInWaves reports whether the application armed the parallel teardown, so the view above can say which of the two orders the waves it lists will actually be closed in. */
func (instance *container) TeardownRunsInWaves() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.teardownInWaves
}

/* TeardownDeadlineOverrun returns timing diagnostics after a teardown exceeds its deadline, including durations, budget consumers and starved closers. It returns nil before teardown, without a deadline or without an overrun. Successful teardown can still expose this record for application logging. */
func (instance *container) TeardownDeadlineOverrun() exceptioncontract.Context {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.teardownDeadline
}

type parallelTeardownArmer interface {
    ArmParallelTeardown() error
}

type teardownPlanner interface {
    TeardownPlan() []containercontract.TeardownPlanEntry
    TeardownRunsInWaves() bool
    TeardownDeadlineOverrun() exceptioncontract.Context
}

type contextCloser interface {
    CloseWithContext(closeContext context.Context) error
}

type closedContainerChecker interface {
    IsClosed() bool
}
