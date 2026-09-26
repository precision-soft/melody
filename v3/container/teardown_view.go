package container

import (
    "sort"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* TeardownPlan answers what the teardown would do if it ran now, without running any of it, under the read lock; it is the door debug:container reads, reached through a type assertion. After Close it still lists every built service, without the inferred edges released with the teardown. */
func (instance *container) TeardownPlan() []containercontract.TeardownPlanEntry {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    plan := instance.teardownPlanLocked()

    entries := make([]containercontract.TeardownPlanEntry, 0, len(plan.closeOrder))

    /* the members of every ring the drain could not open, and nothing else */
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

/* TeardownRunsInWaves reports whether the application armed the parallel teardown. */
func (instance *container) TeardownRunsInWaves() bool {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.teardownInWaves
}

/* TeardownDeadlineOverrun answers the record of a teardown that ran past its deadline, as CloseWithContext describes it, and nil otherwise. It is for the teardown that overran and failed nothing, which returns no error to carry the record; a failed teardown carries the same figures in its error. The answer is a copy. */
func (instance *container) TeardownDeadlineOverrun() exceptioncontract.Context {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.teardownDeadline
}

/* the doors reached through a type assertion on the concrete container, named once so the compiler holds the container to their shapes */
type parallelTeardownArmer interface {
    ArmParallelTeardown() error
}

type teardownPlanner interface {
    TeardownPlan() []containercontract.TeardownPlanEntry
    TeardownRunsInWaves() bool
    TeardownDeadlineOverrun() exceptioncontract.Context
}

type closedContainerChecker interface {
    IsClosed() bool
}
