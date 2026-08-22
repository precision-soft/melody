package container

import (
    containercontract "github.com/precision-soft/melody/v3/container/contract"
)

func WithoutTypeRegistration() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.AlsoRegisterType = false
    }
}

func WithTypeRegistration(isStrict bool) containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.AlsoRegisterType = true
        option.TypeRegistrationIsStrict = isStrict
    }
}

/* WithCollectionPriority orders the registration inside AllImplementing collections: a higher priority is collected — and therefore dispatched by whatever consumes the collection — earlier. The unset priority is zero, so a negative one sorts after every service that declared nothing; registrations sharing a priority keep the stable type-and-name order, so adding a priority to one service never reshuffles the rest. Only a type-registered service can be collected, so the option is meaningless together with WithoutTypeRegistration. */
func WithCollectionPriority(priority int) containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.CollectionPriority = priority
    }
}

/* Replacing admits a SCOPED registration whose name — or whose registered type — the container already holds. Without it the collision is refused where it is made, because a name that means two things depending on where it is asked from is the ambiguity the two lifetimes exist to keep apart. It is read only by the scoped registration paths: a container registration declaring it gains nothing, and takes a scoped-owned name only when the scoped side itself was declared Replacing.

Declaring it where nothing collides is not inert: the waiver is remembered, and a container registration of the same name arriving later is admitted without a declaration of its own. The waiver likewise covers the name and the registered type together — a scoped registration admitted for its name also shadows, inside every scope, the type-keyed resolution of whichever container service shares the type; a registration that means only the name opts out of the type with WithoutTypeRegistration. */
func Replacing() containercontract.RegisterOption {
    return func(option *containercontract.RegisterOptions) {
        option.ReplacesContainerService = true
    }
}

func buildRegisterServiceOption() *containercontract.RegisterOptions {
    return &containercontract.RegisterOptions{
        AlsoRegisterType:         true,
        TypeRegistrationIsStrict: true,
    }
}

func applyRegisterServiceOptions(options []containercontract.RegisterOption) *containercontract.RegisterOptions {
    merged := buildRegisterServiceOption()
    for _, optionFunc := range options {
        if nil == optionFunc {
            continue
        }
        optionFunc(merged)
    }
    return merged
}
