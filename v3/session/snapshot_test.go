package session

import (
    "testing"
)

/* divergentSnapshotSession answers through Snapshot what its accessors do not, so a reading can be told apart by its values */
type divergentSnapshotSession struct {
    accessorOnlySession
}

func (instance *divergentSnapshotSession) Snapshot() (map[string]any, bool, bool) {
    return map[string]any{"door": "snapshot"}, true, true
}

func TestSnapshot_PrefersTheSnapshotDoorOverTheAccessors(t *testing.T) {
    sessionInstance := &divergentSnapshotSession{
        accessorOnlySession: accessorOnlySession{id: "divergent", values: map[string]any{"door": "accessors"}},
    }

    values, modified, cleared := Snapshot(sessionInstance)

    if "snapshot" != values["door"] || false == modified || false == cleared {
        t.Fatalf("expected the snapshot door's reading, got %v %t %t", values, modified, cleared)
    }
}

func TestSnapshot_ReadsASessionWithoutTheDoorThroughItsAccessors(t *testing.T) {
    sessionInstance := &accessorOnlySession{id: "foreign", values: map[string]any{"door": "accessors"}, modified: true}

    values, modified, cleared := Snapshot(sessionInstance)

    if "accessors" != values["door"] || false == modified || true == cleared {
        t.Fatalf("expected the accessors' reading, got %v %t %t", values, modified, cleared)
    }

    sessionInstance.Clear()

    _, _, cleared = Snapshot(sessionInstance)
    if false == cleared {
        t.Fatalf("expected the cleared flag to be read through IsCleared")
    }
}

func TestSnapshot_TheFrameworkSessionImplementsTheSnapshotDoor(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), 0)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("key", "value")

    values, modified, cleared := Snapshot(sessionInstance)
    if "value" != values["key"] || false == modified || true == cleared {
        t.Fatalf("expected the framework session's own reading, got %v %t %t", values, modified, cleared)
    }
}
