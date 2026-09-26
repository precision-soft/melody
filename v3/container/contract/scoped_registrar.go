package contract

/* ScopedRegistrar registers services whose lifetime is one scope: built on the first Get through the scope, held while it lives, closed when it closes, and never seen by the root container. It shares no method with Registrar, so a registrar handed to the wrong hook is a compile error. */
type ScopedRegistrar interface {
    RegisterScoped(serviceName string, provider any, options ...RegisterOption) error

    MustRegisterScoped(serviceName string, provider any, options ...RegisterOption)
}
