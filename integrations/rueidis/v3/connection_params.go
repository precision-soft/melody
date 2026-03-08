package rueidis

import (
    "strings"

    exceptioncontract "github.com/precision-soft/melody/v3/exception/contract"
)

func NewConnectionParams(
    address string,
    user string,
    password string,
) ConnectionParams {
    return ConnectionParams{
        Address:  address,
        User:     user,
        Password: password,
    }
}

type ConnectionParams struct {
    Address  string
    User     string
    Password string
}

func (instance *ConnectionParams) SafeContext() exceptioncontract.Context {
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
