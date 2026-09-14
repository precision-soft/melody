package contract

/* ScopedRegistrar builds services lazily per scope and closes them with that scope. Its method set is disjoint from Registrar so the compiler can reject a registrar with the wrong lifetime. */
type ScopedRegistrar interface {
    RegisterScoped(serviceName string, provider any, options ...RegisterOption) error

    MustRegisterScoped(serviceName string, provider any, options ...RegisterOption)
}
