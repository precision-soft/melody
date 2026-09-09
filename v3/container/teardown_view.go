package container

import (
    "sort"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* TeardownPlan answers what the teardown would do if it ran now, without running any of it. It is a read-only door, declared on the concrete container and reached through a type assertion the way IsClosed and CloseWithContext are, and it is the door `debug:container` reads.

   It takes the container's write lock rather than the read one, because working the plan out merges the edges derived from what each service was seen to HOLD into the graph, which is a write. The cost is one walk, on an operator command. */
func (instance *container) TeardownPlan() []containercontract.TeardownPlanEntry {
    instance.mutex.Lock()
    defer instance.mutex.Unlock()

    plan := instance.teardownPlanLocked()

    entries := make([]containercontract.TeardownPlanEntry, 0, len(plan.closeOrder))

    for _, nodeKey := range plan.closeOrder {
        dependencies := make([]string, 0, len(plan.canonicalEdges[nodeKey]))

        for dependencyKey := range plan.canonicalEdges[nodeKey] {
            dependencies = append(dependencies, dependencyKey)
        }

        sort.Strings(dependencies)

        entries = append(
            entries,
            containercontract.TeardownPlanEntry{
                NodeKey:      nodeKey,
                WaveIndex:    plan.closeWaveIndexOf[nodeKey],
                Dependencies: dependencies,
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
