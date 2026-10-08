package application

import (
    "time"

    "github.com/precision-soft/melody/v3/config"
    configcontract "github.com/precision-soft/melody/v3/config/contract"
)

/* extendedHttpConfigurationOf answers the http configuration's ExtendedHttpConfiguration door, or the defaults each of its methods names when an application's own HttpConfiguration does not implement it. */
func extendedHttpConfigurationOf(httpConfiguration configcontract.HttpConfiguration) configcontract.ExtendedHttpConfiguration {
    extendedHttpConfiguration, isExtended := httpConfiguration.(configcontract.ExtendedHttpConfiguration)
    if true == isExtended {
        return extendedHttpConfiguration
    }

    return defaultExtendedHttpConfiguration{}
}

type defaultExtendedHttpConfiguration struct{}

func (instance defaultExtendedHttpConfiguration) StaticExcludedPaths() []string {
    return nil
}

func (instance defaultExtendedHttpConfiguration) SessionTtl() time.Duration {
    return config.DefaultSessionTtl
}

func (instance defaultExtendedHttpConfiguration) SessionTombstoneRetention() time.Duration {
    return config.DefaultSessionTombstoneRetention
}

func (instance defaultExtendedHttpConfiguration) ShutdownTimeout() time.Duration {
    return config.DefaultHttpShutdownTimeout
}

var _ configcontract.ExtendedHttpConfiguration = defaultExtendedHttpConfiguration{}
