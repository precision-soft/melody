package config

import (
    melodycontainercontract "github.com/precision-soft/melody/v4/container/contract"
)

/* builtAtBootNames answers the names the container builds at boot, in registration order, and the mode each declared */
func builtAtBootNames(containerInstance any) ([]string, map[string]melodycontainercontract.BootFailureMode) {
    registrations := containerInstance.(interface {
        BuiltAtBootRegistrations() []melodycontainercontract.BuiltAtBootRegistration
    }).BuiltAtBootRegistrations()

    names := make([]string, 0, len(registrations))
    modes := make(map[string]melodycontainercontract.BootFailureMode, len(registrations))
    for _, registration := range registrations {
        names = append(names, registration.ServiceName)
        modes[registration.ServiceName] = registration.BootFailure
    }

    return names, modes
}
