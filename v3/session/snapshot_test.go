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

/* clearingBetweenReadsSession clears itself right after its first accessor read, whichever accessor Snapshot reads first: the interleaving a concurrent Clear produces between the three reads */
type clearingBetweenReadsSession struct {
    accessorOnlySession
    reads int
}

func (instance *clearingBetweenReadsSession) afterRead() {
    instance.reads++
    if 1 == instance.reads {
        instance.Clear()
    }
}

func (instance *clearingBetweenReadsSession) All() map[string]any {
    defer instance.afterRead()

    return instance.accessorOnlySession.All()
}

func (instance *clearingBetweenReadsSession) IsModified() bool {
    defer instance.afterRead()

    return instance.accessorOnlySession.IsModified()
}

func (instance *clearingBetweenReadsSession) IsCleared() bool {
    defer instance.afterRead()

    return instance.accessorOnlySession.IsCleared()
}

/* a Clear landing between the three reads is answered as cleared: the latched flag is read last, so the response path deletes the session rather than saving it, modified, under its id */
func TestSnapshot_AnswersAClearLandingBetweenTheReadsAsCleared(t *testing.T) {
    sessionInstance := &clearingBetweenReadsSession{
        accessorOnlySession: accessorOnlySession{id: "racing", values: map[string]any{"user": "alice"}, modified: true},
    }

    _, _, cleared := Snapshot(sessionInstance)

    if 3 != sessionInstance.reads {
        t.Fatalf("expected the three accessor reads, got %d", sessionInstance.reads)
    }
    if false == cleared {
        t.Fatalf("expected the clear landing after the first read to be answered as cleared")
    }
}
