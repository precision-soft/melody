package contract

/* Serializer turns a cached value into the payload the backend stores, and a payload back into a value of the type the reader names. Deserialize decodes into target, a non-nil pointer: a *T is filled with a T, and a *any with the encoding's own generic form (for json a map for an object, a float64 for a number), which is how a caller that does not know the type reads. A payload that does not decode into the target's type is an error, never a partial success. The manager always hands a pointer to a zero value it made itself, so an implementation need not clear the target. Implementations are safe for concurrent use, return bytes the caller owns and keep no alias of the payload. */
type Serializer interface {
    Serialize(value any) ([]byte, error)

    Deserialize(payload []byte, target any) error
}
