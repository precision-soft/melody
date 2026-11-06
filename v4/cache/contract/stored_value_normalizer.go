package contract

/* StoredValueNormalizer is the optional door through which Remember learns what shape a stored value reads back as, so the computing call answers what every cached call answers. The manager implements it with a local serializer round-trip; it is asked of the Cache handed to Remember, so a decorator over the manager forwards it too. */
type StoredValueNormalizer interface {
    NormalizeStoredValue(value any) (any, error)
}
