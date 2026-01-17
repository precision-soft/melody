package config

import (
	"strings"

	configcontract "github.com/precision-soft/melody/config/contract"
	"github.com/precision-soft/melody/exception"
)

func newCliConfiguration(name string, description string) (*cliConfiguration, error) {
	cliConfigurationInstance := &cliConfiguration{
		name:        strings.TrimSpace(name),
		description: strings.TrimSpace(description),
	}

	validateErr := cliConfigurationInstance.validate()
	if nil != validateErr {
		return nil, validateErr
	}

	return cliConfigurationInstance, nil
}

type cliConfiguration struct {
	name        string
	description string
}

func (instance *cliConfiguration) Name() string {
	return instance.name
}

func (instance *cliConfiguration) Description() string {
	return instance.description
}

func (instance *cliConfiguration) validate() error {
	if "" == instance.name {
		return exception.NewError(
			"cli name may not be empty",
			nil,
			nil,
		)
	}

	return nil
}

var _ configcontract.CliConfiguration = (*cliConfiguration)(nil)
