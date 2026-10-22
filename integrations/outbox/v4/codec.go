package outbox

/* MessageCodec encodes a domain message into a durable form for the outbox and decodes it back when the relay publishes it. The application supplies it because only the application knows its message types; a JSON codec over a small type registry is the typical implementation. */
type MessageCodec interface {
    Encode(message any) (typeName string, payload []byte, err error)

    Decode(typeName string, payload []byte) (any, error)
}
