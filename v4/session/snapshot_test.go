package session

import (
    "testing"
)

/* divergentSnapshotSession answers through Snapshot what its accessors do not, so a reading can be told apart by its values */
type divergentSnapshotSession struct {
    foreignSession
}

func (instance *divergentSnapshotSession) Snapshot() (map[string]any, bool, bool) {
    return map[string]any{"door": "snapshot"}, true, true
}

func TestSnapshot_AnswersTheSessionsOwnSnapshot(t *testing.T) {
    sessionInstance := &divergentSnapshotSession{
        foreignSession: foreignSession{id: "divergent", values: map[string]any{"door": "accessors"}},
    }

    values, modified, cleared := Snapshot(sessionInstance)

    if "snapshot" != values["door"] || false == modified || false == cleared {
        t.Fatalf("expected the session's own snapshot, got %v %t %t", values, modified, cleared)
    }
}

func TestSnapshot_ReadsTheFrameworkSession(t *testing.T) {
    manager := NewManager(NewInMemoryStorage(), 0)

    sessionInstance := manager.NewSession()
    sessionInstance.Set("key", "value")

    values, modified, cleared := Snapshot(sessionInstance)
    if "value" != values["key"] || false == modified || true == cleared {
        t.Fatalf("expected the framework session's own reading, got %v %t %t", values, modified, cleared)
    }
}
