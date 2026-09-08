package amqp

import "testing"

/* The two doors of a turn are a protocol whose whole content is which of them wins, and the instant they decide in cannot be produced from outside: a caller's timer firing against a write that has just begun is a tie between two goroutines. So each transition is handed the state directly instead of being raced into (§5.34) — the same manoeuvre this package already uses for the branch a write budget expires into. */

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
