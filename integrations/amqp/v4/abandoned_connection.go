package amqp

import (
    "time"

    amqp091 "github.com/rabbitmq/amqp091-go"
)

/* dialOutcome carries what a dialer answered, from the goroutine that ran it. */
type dialOutcome struct {
    connection *amqp091.Connection
    err        error
}

/* closeAbandonedConnection closes, on a goroutine of its own and under the deadline, a connection that belongs to nobody — one a dial answered after Close began — since the close is an RPC to a broker that may not answer, and run on the caller it would hold the Close or the reconnect that found it. The goroutine outlives the caller by at most the deadline. */
func closeAbandonedConnection(connection *amqp091.Connection, deadline time.Time) {
    go func() {
        _ = connection.CloseDeadline(deadline)
    }()
}

/* drainAbandonedDial waits, on a goroutine of its own, for a dial its caller stopped waiting for, and closes the connection it answers under the budget counted from its landing, so the late connection cannot leak and its close cannot outlive that budget on a broker that does not answer. */
func drainAbandonedDial(outcome <-chan dialOutcome, closeBudget time.Duration) {
    go func() {
        result := <-outcome
        if nil != result.connection {
            _ = result.connection.CloseDeadline(time.Now().Add(closeBudget))
        }
    }()
}
