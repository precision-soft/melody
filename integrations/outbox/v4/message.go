package outbox

/* DefaultTableName is the outbox table a store left unconfigured reads and writes. */
const DefaultTableName = "melody_outbox"

const (
    StatusPending  = "pending"
    StatusInFlight = "inflight"
    StatusSent     = "sent"
    StatusDead     = "dead"
)

/* Pending is a stored outbox row ready to be relayed: the codec type name and payload needed to rebuild the message, plus the retry counters. Attempts is how many sends have failed (drives backoff and the MaxAttempts dead-letter); DeliveryAttempts is the row's stored delivery-attempt count at claim time — the relay advances it per row at delivery time (RecordDeliveryAttempt) and the post-increment value, not this snapshot, drives the MaxDeliveryAttempts crash-poison cap. ClaimToken is the fencing token stamped on the row by the claim that produced this Pending; the relay passes it back to every transition write so a stale run cannot clobber a row another instance has since re-claimed. Headers is the stored JSON object of the enqueue's headers, empty for none, which the relay decodes into a messagebus.HeadersStamp; Transport is the destination the row was enqueued for, empty for none; Table is the table the row was claimed from, empty or DefaultTableName for the default one, and carried into the message id. */
type Pending struct {
    Id               int64
    TypeName         string
    Payload          []byte
    Attempts         int
    DeliveryAttempts int
    ClaimToken       string
    Headers          string
    Transport        string
    Table            string
}
