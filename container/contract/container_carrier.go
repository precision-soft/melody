package contract

/* ContainerCarrier is implemented by resolvers that can name the container behind them. A process-lifetime service that defers work past the resolution that built it replays that work through the container, since a resolution context is single-threaded and ends with its scope. The container answers itself. */
type ContainerCarrier interface {
    Container() Container
}
