package contract

/* TeardownPlanEntry is one service as the teardown will meet it: its node, its wave and the services it is closed before. The door that answers it stays on the concrete container, reached through a type assertion. */
type TeardownPlanEntry struct {
    /* NodeKey is the service as the teardown graph knows it: "service:<name>" for a registration filed under a name, "type:<identity>" for one filed only under its type. */
    NodeKey string
    /* WaveIndex is the wave that closes it, from zero, under the armed teardown. The sequential teardown honours every edge but closes in the drain's own order, not by wave. */
    WaveIndex int
    /* Dependencies are the services this one is closed before, in the graph's key space. Empty does not mean unordered: its dependents may still order it. */
    Dependencies []string
    /* SerialGroup is the group, from one, this service closes one after the other with inside its wave, zero for none: services seen holding each other carry no edge but are not unrelated. */
    SerialGroup int
    /* Aliases are the other node keys the same instance is filed under, collapsed onto this entry. */
    Aliases []string
    /* Cycle reports that the service is on a ring the drain could not open. A ring closes as one unit in a wave of its own, members one after the other in creation order, latest first, and the drain continues past it. Its Dependencies list every edge; the flag, not the list, tells the ring from a proved order. */
    Cycle bool
}
