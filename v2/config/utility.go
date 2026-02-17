package config

import (
	configcontract "github.com/precision-soft/melody/v2/config/contract"
)

func IntWithDefault(configParameter configcontract.Parameter, defaultValue int) int {
	if nil == configParameter {
		return defaultValue
	}

	value, err := configParameter.Int()
	if nil != err {
		return defaultValue
	}

	return value
}
