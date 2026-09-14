package contract

/* TeardownPlanEntry describes a built service in the container’s current teardown plan. The concrete container exposes the plan as an optional capability. */
type TeardownPlanEntry struct {
    /* NodeKey is the service as the teardown graph knows it: "service:<name>" for a registration filed under a name, "type:<identity>" for one filed only under its type. */
    NodeKey string
    /* WaveIndex is zero-based. Waves retain their order when parallel teardown is disabled; arming permits concurrency within a wave. */
    WaveIndex int
    /* Dependencies lists outgoing graph edges: services closed after this one. An empty list does not exclude incoming dependencies or serial-group constraints. */
    Dependencies []string
    /* SerialGroup identifies services closed sequentially within one wave; zero means no group. Mutual captured references can require serialization without an ordering edge. */
    SerialGroup int
    /* Aliases lists other graph keys collapsed onto this instance, which is closed once under NodeKey. */
    Aliases []string
    /* Cycle marks members of a strongly connected component. The component takes its own wave and closes in reverse creation order; dependencies outside it are released afterwards without receiving the cycle flag. Dependencies remains the full outgoing edge list. */
    Cycle bool
}
