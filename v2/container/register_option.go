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
