package contract

type Serializer interface {
    Serialize(value any) ([]byte, error)

    Deserialize(payload []byte) (any, error)
}
