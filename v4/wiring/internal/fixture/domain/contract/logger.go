package contract

/* Logger exercises a dependency the generated wiring must address through a qualified type from a nested package. */
type Logger interface {
    Log(message string)
}
