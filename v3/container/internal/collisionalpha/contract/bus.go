package contract

/* Bus and its beta twin share the type String() "contract.Bus" while being distinct types from distinct packages, the shape that used to alias the resolution keys. */
type Bus struct {
    Region string
}
