package rueidis

import (
    "strings"

    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

func NewConnectionParameters(
    address string,
    user string,
    password string,
) ConnectionParameters {
    return ConnectionParameters{
        Address:  address,
        User:     user,
        Password: password,
    }
}

type ConnectionParameters struct {
    Address  string
    User     string
    Password string
}

func (instance *ConnectionParameters) SafeContext() exceptioncontract.Context {
    return exceptioncontract.Context{
        "address": instance.Address,
        "user":    instance.User,
    }
}

func parseAddressList(value string) []string {
    trimmedValue := strings.TrimSpace(value)
    if "" == trimmedValue {
        return nil
    }

    parts := strings.Split(trimmedValue, ",")
    addresses := make([]string, 0, len(parts))
    for _, part := range parts {
        address := strings.TrimSpace(part)
        if "" == address {
            continue
        }

        addresses = append(addresses, address)
    }

    return addresses
}

/* Deprecated: use ConnectionParameters. */
type ConnectionParams = ConnectionParameters

/* Deprecated: use NewConnectionParameters. */
func NewConnectionParams(
    address string,
    user string,
    password string,
) ConnectionParameters {
    return NewConnectionParameters(address, user, password)
}
