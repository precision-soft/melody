package contract

/* TeardownPlanEntry is one service as the container's teardown will meet it: the node it is filed under, the wave it belongs to, and the services it is closed before.

   It lives in the contract package because it is a shape a consumer READS — the debug command renders it, and an application arming the parallel teardown checks it — while the door that answers it stays on the concrete container, reached through a type assertion, for the reason written at IsClosed. */
type TeardownPlanEntry struct {
    /* NodeKey is the service as the teardown graph knows it: "service:<name>" for a registration filed under a name, "type:<identity>" for one filed only under its type. */
    NodeKey string
    /* WaveIndex is which wave closes it, counting from zero. The waves are the order the ARMED teardown runs. The sequential teardown honours every edge but does not walk the waves: it closes in the drain's own order, creation latest-first among what is free, so a service of wave zero can close after one of wave one it has no relation to — measured, `[c a b]` over waves `c:0 a:1 b:0`. Under waves what changes is that the members of one wave are closed together, in wave order. */
    WaveIndex int
    /* Dependencies are the services this one is closed BEFORE, in the graph's own key space. EMPTY means it is closed before nothing — it may still be ordered by its DEPENDENTS, whose edges are written at them: a pure dependency such as the logger has an empty list and is anything but unordered. What nothing orders is a service with no edge on EITHER side, which is what `debug:container` reads as `ordering: none`. */
    Dependencies []string
    /* SerialGroup is the group of services this one is closed one after the other WITH inside its wave, counting from one, and zero for a service in no group. Two services seen holding each other — or a ring of them — carry no edge, because no ordering between them is true, but they are not unrelated: a wave closes such a group one service at a time while the rest of the wave starts together. Without the figure the view read "same wave, no dependencies" for a pair the teardown deliberately keeps apart. */
    SerialGroup int
    /* Aliases are the other node keys the same instance is filed under — a service registered under a name and resolved through its type, or two names handed one pointer — which the plan collapsed onto this node: the teardown meets the instance once, under NodeKey, and a reader asking by any of the aliases is asking about this entry. */
    Aliases []string
    /* Cycle reports that the service is on a ring the drain could not open — a strongly connected component of two or more services, each depending on another of them. A ring is closed as one unit, in a wave of its own that runs its members one after the other in creation order, latest first, and the teardown reports it; the drain then continues past it, so a pure dependency of a ring member — ordered after it, on no ring itself — is released by the ring's close and takes its place in the waves the graph proves, unflagged. A cycle member's Dependencies list every edge it has — the ring's and a pure dependency's alike (measured: `a` on the ring `a↔b` with `d` resolved later lists `[b d]`); the flag, not the list, is what tells the ring from a proved order; an edge the walk inferred never lies on a ring, because a held pair with a way back is kept apart as unordered rather than written as an edge. */
    Cycle bool
}
