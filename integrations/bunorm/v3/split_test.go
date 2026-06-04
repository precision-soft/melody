package bunorm

import (
    "testing"
)

func TestReadWriteSplitter_WriterIsPrimaryReaderRoundRobins(t *testing.T) {
    splitter := &ReadWriteSplitter{
        primaryName:  "primary",
        replicaNames: []string{"replica-a", "replica-b"},
    }

    if "primary" != splitter.WriterName() {
        t.Fatalf("unexpected writer name: %s", splitter.WriterName())
    }

    expected := []string{"replica-a", "replica-b", "replica-a", "replica-b"}
    for index, want := range expected {
        got := splitter.ReaderName()
        if want != got {
            t.Fatalf("round-robin position %d: expected %s, got %s", index, want, got)
        }
    }
}

func TestReadWriteSplitter_ReaderFallsBackToPrimaryWithoutReplicas(t *testing.T) {
    splitter := &ReadWriteSplitter{
        primaryName: "primary",
    }

    if "primary" != splitter.ReaderName() {
        t.Fatalf("expected reader to fall back to primary, got %s", splitter.ReaderName())
    }
}
