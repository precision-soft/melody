package container

import (
    "math/rand"
    "testing"

    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

type reflectionGraphNode struct {
    left  *reflectionGraphNode
    right *reflectionGraphNode
    value int
}

func TestHeldPointerIdentities_MatchesBoundedGraphReachability(t *testing.T) {
    random := rand.New(rand.NewSource(20260911))
    for sample := 0; sample < 500; sample++ {
        const size = 18
        nodes := make([]*reflectionGraphNode, size)
        for index := range nodes {
            nodes[index] = &reflectionGraphNode{value: index}
        }
        edges := make([][2]int, size)
        for index, node := range nodes {
            edges[index] = [2]int{random.Intn(size + 1) - 1, random.Intn(size + 1) - 1}
            if 0 <= edges[index][0] {
                node.left = nodes[edges[index][0]]
            }
            if 0 <= edges[index][1] {
                node.right = nodes[edges[index][1]]
            }
        }

        distances := make([]int, size)
        for index := range distances {
            distances[index] = -1
        }
        distances[0] = 0
        pending := []int{0}
        for head := 0; head < len(pending); head++ {
            current := pending[head]
            for _, next := range edges[current] {
                if 0 <= next && 0 > distances[next] {
                    distances[next] = distances[current] + 1
                    pending = append(pending, next)
                }
            }
        }
        actual := heldPointerIdentities(nodes[0])
        for index, node := range nodes {
            identity, _ := pointerKeyOf(node)
            /* Each graph edge crosses a struct and then its pointer field. */
            wanted := 0 <= distances[index] && 2 * distances[index] <= teardownWalkDepthLimit
            if wanted != holdsIdentity(actual, identity) {
                t.Fatalf("sample %d node %d distance %d: wanted held=%v; edges=%v", sample, index, distances[index], wanted, edges)
            }
        }
    }
}

func (instance *reflectionGraphNode) Close() error { return nil }

func TestContainer_TeardownPlanRecognizesReachableCollaboratorThroughCycles(t *testing.T) {
    edges := [][2]int{{7, 14}, {14, 16}, {14, 14}, {14, 1}, {17, 14}, {1, 3}, {13, 9}, {9, 0}, {9, 6}, {9, 10}, {16, 14}, {8, 8}, {16, 17}, {6, 14}, {10, -1}, {5, 0}, {1, 11}, {0, 7}}
    nodes := make([]*reflectionGraphNode, len(edges))
    for index := range nodes {
        nodes[index] = &reflectionGraphNode{value: index}
    }
    for index, edge := range edges {
        if 0 <= edge[0] {
            nodes[index].left = nodes[edge[0]]
        }
        if 0 <= edge[1] {
            nodes[index].right = nodes[edge[1]]
        }
    }
    serviceContainer := NewContainer()
    armParallelTeardown(t, serviceContainer)
    serviceContainer.MustRegister("graph.root", func(_ containercontract.Resolver) (*reflectionGraphNode, error) { return nodes[0], nil }, WithoutTypeRegistration())
    serviceContainer.MustRegister("graph.peer", func(_ containercontract.Resolver) (*reflectionGraphNode, error) { return nodes[6], nil }, WithoutTypeRegistration())
    MustFromResolver[*reflectionGraphNode](serviceContainer, "graph.root")
    MustFromResolver[*reflectionGraphNode](serviceContainer, "graph.peer")
    waves := make(map[string]int)
    for _, entry := range serviceContainer.(interface {
        TeardownPlan() []containercontract.TeardownPlanEntry
    }).TeardownPlan() {
        waves[entry.NodeKey] = entry.WaveIndex
    }
    rootWave, rootPresent := waves["service:graph.root"]
    peerWave, peerPresent := waves["service:graph.peer"]
    if false == rootPresent || false == peerPresent || rootWave >= peerWave {
        t.Fatalf("reachable collaborator must close after its holder: %v", waves)
    }
    if err := serviceContainer.Close(); nil != err {
        t.Fatal(err)
    }
}
