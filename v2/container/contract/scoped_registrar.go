package contract

/* ScopedRegistrar registers services whose lifetime is one scope: built on the first Get through the scope, closed when it closes, and invisible to the root container. It does not embed Registrar and the two method sets are disjoint, so a registrar handed to the wrong hook is a compile error. */
type ScopedRegistrar interface {
    RegisterScoped(serviceName string, provider any, options ...RegisterOption) error

    MustRegisterScoped(serviceName string, provider any, options ...RegisterOption)
}
