package contract

/* StoredValueNormalizer is the optional door through which Remember learns what shape a value stored in a Cache reads back as, so the computing call answers exactly what every cached call answers. The manager implements it with one local serializer round-trip; the door is asked of the Cache value Remember was handed, so a decorator over the manager implements it too — forwarding to the decorated manager — or the miss answers the callback's own value again and the hit the decoded one, the two shapes the door exists to make one. */
type StoredValueNormalizer interface {
    NormalizeStoredValue(value any) (any, error)
}
