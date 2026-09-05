package contract

/* ScopedRegistrar registers services whose lifetime is one scope: built lazily on the first Get through that scope, held for as long as it lives, closed when it closes. The root container never sees them, and two scopes resolving the same name never meet.

   It is deliberately not a Registrar, and it does not embed one. The two registrations differ only in lifetime, and a provider handed to the wrong one is a mistake the compiler cannot see when both spell the same verb: a container provider registered as scoped is rebuilt and torn down once per request without ever failing. Go interfaces are structural, so a marker type carrying Registrar's own method set would be satisfied by a container registrar and would catch nothing in either direction. The verb at the call site is therefore the declaration, and the two method sets are kept disjoint so a registrar handed to the wrong hook is a compile error rather than a convention. */
type ScopedRegistrar interface {
    RegisterScoped(serviceName string, provider any, options ...RegisterOption) error

    MustRegisterScoped(serviceName string, provider any, options ...RegisterOption)
}
