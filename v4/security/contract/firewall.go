package contract

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
    httpcontract "github.com/precision-soft/melody/v4/http/contract"
)

type Firewall interface {
    Name() string

    LoginPath() string

    LogoutPath() string

    Login(
        runtimeInstance containercontract.Runtime,
        request httpcontract.Request,
        input LoginInput,
    ) (*LoginResult, error)

    Logout(
        runtimeInstance containercontract.Runtime,
        request httpcontract.Request,
        input LogoutInput,
    ) (*LogoutResult, error)
}
