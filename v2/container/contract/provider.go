package contract

type Provider[T any] func(resolver Resolver) (T, error)
