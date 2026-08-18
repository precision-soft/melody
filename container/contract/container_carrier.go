package contract

/* ContainerCarrier is implemented by resolvers that can name the container behind them. A process-lifetime service that defers work past the resolution that built it — a lazily-dialed pool, a registry that opens on first use — replays that work through the container, not through the resolution context it was constructed with: a resolution context is single-threaded and dies with its scope, while the container is the process-lifetime, lock-guarded half the deferred work actually needs. The container itself satisfies the shape by answering itself. */
type ContainerCarrier interface {
    Container() Container
}
