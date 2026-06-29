package outbox

/* TableName is the outbox table the Store reads and writes. */
const TableName = "melody_outbox"

const (
    StatusPending  = "pending"
    StatusInFlight = "inflight"
    StatusSent     = "sent"
    StatusDead     = "dead"
)

/* Pending is a stored outbox row ready to be relayed: the codec type name and payload needed to rebuild the message, plus how many delivery attempts have already been made. */
type Pending struct {
    Id       int64
    TypeName string
    Payload  []byte
    Attempts int
}
