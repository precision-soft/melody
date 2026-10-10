package contract

/* Serializer transforms values to and from their byte representation for one media type. Implementations must be safe for concurrent use: the framework wires each serializer as a process-wide singleton and every request goroutine calls it simultaneously, so an implementation holding reusable state — a buffer, a cached encoder, a memoization map — must synchronize it. Serialize returns bytes the caller owns and Deserialize leaves the target owning its bytes; neither side may retain or alias the other's buffers. */
type Serializer interface {
    Serialize(value any) ([]byte, error)

    Deserialize(payload []byte, target any) error

    ContentType() string
}
