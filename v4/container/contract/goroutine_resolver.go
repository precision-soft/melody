package contract

/* GoroutineResolver is the optional capability of the resolver a provider receives to hand out a view for a goroutine the provider starts: every resolution through that view runs on a chain of its own, even while the provider is still running, and is still recorded as the provider's dependency. The container and a scope do not implement it, so container.ForGoroutine answers them as they are. */
type GoroutineResolver interface {
    ForGoroutine() Resolver
}
