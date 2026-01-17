package validation

import (
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
)

func ValidatorMustFromContainer(serviceContainer containercontract.Container) *Validator {
	return container.MustFromResolver[*Validator](serviceContainer, ServiceValidator)
}

func ValidatorFromContainer(serviceContainer containercontract.Container) *Validator {
	validatorInstance, err := container.FromResolver[*Validator](serviceContainer, ServiceValidator)
	if nil == validatorInstance || nil != err {
		return nil
	}

	return validatorInstance
}
