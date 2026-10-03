package contract

/* Bus and its beta twin share the type String() "contract.Bus" while being distinct types from distinct packages, the pair the type identity key has to tell apart. */
type Bus struct {
    Region string
}
