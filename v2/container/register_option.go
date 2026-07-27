package container

import (
    containercontract "github.com/precision-soft/melody/v2/container/contract"
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

/* Replacing admits a registration whose name — or whose registered type — the other level already holds: a scoped registration taking a name the container owns, or a container registration taking one a scoped registration owns. Without it the collision is refused where it is made, because a name that means two things depending on where it is asked from is the ambiguity the two lifetimes exist to keep apart.

Declaring it where nothing collides is inert rather than an error, so a registration made conditionally does not have to know whether the other level got there first. */
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
