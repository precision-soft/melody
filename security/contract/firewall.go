package contract

import (
	httpcontract "github.com/precision-soft/melody/http/contract"
	runtimecontract "github.com/precision-soft/melody/runtime/contract"
)

type Firewall interface {
	Name() string

	LoginPath() string

	LogoutPath() string

	Login(
		runtimeInstance runtimecontract.Runtime,
		request httpcontract.Request,
		input LoginInput,
	) (*LoginResult, error)

	Logout(
		runtimeInstance runtimecontract.Runtime,
		request httpcontract.Request,
		input LogoutInput,
	) (*LogoutResult, error)
}
