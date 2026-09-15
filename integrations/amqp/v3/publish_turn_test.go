package amqp

import "testing"


func TestPublishTurn_ACallerThatGivesUpAfterTheWriteBeganIsToldItMayNot(t *testing.T) {
    turn := newPublishTurn()

    if false == turn.begin() {
        t.Fatalf("a write with a caller still waiting must be allowed to begin")
    }

    if true == turn.abandon() {
        t.Fatalf("a caller whose write had already begun was told it lost its TURN, so a publish that is on the socket is reported as one that never reached it")
    }
}

func TestPublishTurn_AWriteThatFindsItsCallerGoneDoesNotBegin(t *testing.T) {
    turn := newPublishTurn()

    if false == turn.abandon() {
        t.Fatalf("a caller whose write had not begun must be allowed to give up")
    }

    if true == turn.begin() {
        t.Fatalf("the write began for a caller that had already been told the message did not go out")
    }
}

func TestPublishTurn_StartedAnnouncesTheWriteAndNothingBefore(t *testing.T) {
    turn := newPublishTurn()

    select {
    case <-turn.started():
        t.Fatalf("the turn announced a write that had not begun")
    default:
    }

    turn.begin()

    select {
    case <-turn.started():
    default:
        t.Fatalf("the turn did not announce the write that began, so a caller whose timer fired against it waits for a signal that never comes")
    }
}
