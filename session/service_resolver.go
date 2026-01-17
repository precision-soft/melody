package session

import (
	"github.com/precision-soft/melody/container"
	containercontract "github.com/precision-soft/melody/container/contract"
	sessioncontract "github.com/precision-soft/melody/session/contract"
)

const (
	ServiceSessionManager = "service.session.manager"
	ServiceSessionStorage = "service.session.storage"
)

func SessionMustFromContainer(serviceContainer containercontract.Container) sessioncontract.Manager {
	return container.MustFromResolver[sessioncontract.Manager](serviceContainer, ServiceSessionManager)
}

func SessionStorageMustFromContainer(serviceContainer containercontract.Container) sessioncontract.Storage {
	return container.MustFromResolver[sessioncontract.Storage](serviceContainer, ServiceSessionStorage)
}

func SessionStorageMustFromResolver(resolver containercontract.Resolver) sessioncontract.Storage {
	return container.MustFromResolver[sessioncontract.Storage](resolver, ServiceSessionStorage)
}
