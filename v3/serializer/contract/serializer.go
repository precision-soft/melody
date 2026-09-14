package contract

/* Serializer transforms values to and from bytes for one media type. Implementations are shared across requests and must support concurrent calls. Serialize returns caller-owned bytes; Deserialize gives the target independent ownership of its bytes. Neither operation may retain or alias the other party’s buffers. */
type Serializer interface {
    Serialize(value any) ([]byte, error)

    Deserialize(payload []byte, target any) error

    ContentType() string
}
