package container

import (
    "context"
    "sort"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

/* TeardownPlan answers what the teardown would do if it ran now, without running any of it. It is a read-only door, declared on the concrete container and reached through a type assertion the way IsClosed and CloseWithContext are, and it is the door `debug:container` reads.

   It takes the container's read lock, because working the plan out writes nothing: the edges derived from what each service holds and from what each declared type stands for are the plan's own, computed for this answer and forgotten with it. Written into the graph instead, they outlived the state they were drawn from, and a resolution or a registration made after the view closed a ring nothing had declared. The cost is one plan, on an operator command.

   After Close the answer still lists every service the container built — the instances stay filed, for the resolver's refusals — but without the edges the walk had inferred, whose records were released with the teardown; it is not the plan the teardown ran, which was computed once, under the write lock, and is not kept. */
func (instance *container) TeardownPlan() []containercontract.TeardownPlanEntry {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    plan := instance.teardownPlanLocked()

    entries := make([]containercontract.TeardownPlanEntry, 0, len(plan.closeOrder))

    /* the members of every ring the drain could not open, and nothing else: a pure dependency of a ring member is released once the ring is closed and takes its place in the order the graph proves, so it is not flagged — flagged, the operator looked for a ring the dependency was not on; unflagged where the remainder was closed whole in creation order, the view read as proved an order the teardown did not honour */
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

/* TeardownDeadlineOverrun answers the record of a teardown that ran past the deadline it was given, and nil for one that had no deadline, finished inside it, or has not run: the budget, what was spent, the closers running when the deadline passed, the closers reached after it, and the duration of every close, as CloseWithContext describes them. It exists for the teardown that overran and FAILED NOTHING — a closer reached with the budget gone answers nil where it has nothing left to do — because that teardown returns no error to carry the record, and a container has no journal of its own; the application reads this door after a clean close and writes the record as a warning. A teardown that failed carries the same record beside its failures, in the error. */
func (instance *container) TeardownDeadlineOverrun() exceptioncontract.Context {
    instance.mutex.RLock()
    defer instance.mutex.RUnlock()

    return instance.teardownDeadline
}

/* The doors below are reached through a type assertion on the concrete container, because none of them is declared on the Container contract, for the reason written at IsClosed: a method added to the contract is a method every application carrying its own implementation would have to grow. Naming their shapes here, once, gives the compiler something to hold the container to — a door renamed or re-signed fails this package instead of turning every assertion at its callers into a silent false. The callers keep spelling the assertion themselves, since importing a name from here would couple them to the implementation the contract exists to hide. */
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
