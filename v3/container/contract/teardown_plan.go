package contract

/* TeardownPlanEntry is one service as the container's teardown will meet it: the node it is filed under, the wave it belongs to, and the services it is closed before.

   It lives in the contract package because it is a shape a consumer READS — the debug command renders it, and an application arming the parallel teardown checks it — while the door that answers it stays on the concrete container, reached through a type assertion, for the reason written at IsClosed. */
type TeardownPlanEntry struct {
    /* NodeKey is the service as the teardown graph knows it: "service:<name>" for a registration filed under a name, "type:<identity>" for one filed only under its type. */
    NodeKey string
    /* WaveIndex is which wave closes it, counting from zero. The waves are walked in order whether or not the parallel teardown is armed, so the figure describes both worlds; what arming changes is whether the members of one wave are closed together. */
    WaveIndex int
    /* Dependencies are the services this one is closed BEFORE, in the graph's own key space. EMPTY means nothing in this container orders this service against any other — neither a resolution, nor a declaration, nor a collaborator it was seen to hold — so under waves it closes beside everything else in its wave. */
    Dependencies []string
    /* SerialGroup is the group of services this one is closed one after the other WITH inside its wave, counting from one, and zero for a service in no group. Two services seen holding each other — or a ring of them — carry no edge, because no ordering between them is true, but they are not unrelated: a wave closes such a group one service at a time while the rest of the wave starts together. Without the figure the view read "same wave, no dependencies" for a pair the teardown deliberately keeps apart. */
    SerialGroup int
    /* Aliases are the other node keys the same instance is filed under — a service registered under a name and resolved through its type, or two names handed one pointer — which the plan collapsed onto this node: the teardown meets the instance once, under NodeKey, and a reader asking by any of the aliases is asking about this entry. */
    Aliases []string
    /* Cycle reports that the service is left over by a dependency cycle the drain could not resolve: it is closed one service at a time with the rest of the remainder, in the remainder's own order, and the teardown reports the cycle. A cycle member's Dependencies are the edges it declared or resolved that lead back to it, which is what makes it a cycle — the figure is what tells that reading from a proved order; an edge the walk inferred never lies on a ring, because a held pair with a way back is kept apart as unordered rather than written as an edge. A pure dependency of a ring member — ordered after it, on no ring itself — is closed with the remainder but is not flagged. */
    Cycle bool
}
