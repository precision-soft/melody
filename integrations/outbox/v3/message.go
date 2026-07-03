package outbox

/* TableName is the outbox table the Store reads and writes. */
const TableName = "melody_outbox"

const (
    StatusPending  = "pending"
    StatusInFlight = "inflight"
    StatusSent     = "sent"
    StatusDead     = "dead"
)

/* Pending is a stored outbox row ready to be relayed: the codec type name and payload needed to rebuild the message, plus the retry counters. Attempts is how many sends have failed (drives backoff and the MaxAttempts dead-letter); DeliveryAttempts is the row's stored delivery-attempt count at claim time — the relay advances it per row at delivery time (RecordDeliveryAttempt) and the post-increment value, not this snapshot, drives the MaxDeliveryAttempts crash-poison cap. */
type Pending struct {
    Id               int64
    TypeName         string
    Payload          []byte
    Attempts         int
    DeliveryAttempts int
}
