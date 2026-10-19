package config

import (
    "github.com/precision-soft/melody/v3/.example/subscriber"
    melodycontainercontract "github.com/precision-soft/melody/v3/container/contract"
)

/* bootServiceNameList names the services the process builds at boot rather than at first use. The catalogue notification hub is built by the module before the container exists and handed its journal by its provider, which only the nomenclature listener resolves, on a write: a process serving only readers would run the hub without a journal, its failed backplane publishes and dropped events counted into atomics nobody reads. */
var bootServiceNameList = []string{
    subscriber.ServiceCatalogNotificationHub,
}

/* ResolveBootServices builds the services of bootServiceNameList through the container, so the providers that install what they need run before the first request. The composition root calls it after Boot and before the teardown is armed, so the arming walks these services too. */
func ResolveBootServices(resolver melodycontainercontract.Resolver) error {
    for _, serviceName := range bootServiceNameList {
        if _, resolveErr := resolver.Get(serviceName); nil != resolveErr {
            return resolveErr
        }
    }

    return nil
}
