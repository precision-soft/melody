package bunorm

import (
	"sync"

	"github.com/uptrace/bun"

	"github.com/precision-soft/melody/exception"
	loggingcontract "github.com/precision-soft/melody/logging/contract"
)

type ManagerRegistry struct {
	logger loggingcontract.Logger

	providerDefinitionByName      map[string]ProviderDefinition
	defaultProviderDefinitionName string

	lock     sync.Mutex
	managers map[string]*Manager
}

func NewManagerRegistry(logger loggingcontract.Logger, providerDefinitions ...ProviderDefinition) (*ManagerRegistry, error) {
	if nil == logger {
		return nil, ErrLoggerIsRequired
	}

	if 0 == len(providerDefinitions) {
		return nil, ErrNoProviderDefinitions
	}

	providerDefinitionByName := make(map[string]ProviderDefinition, len(providerDefinitions))
	defaultProviderDefinitionName := ""
	defaultCount := 0

	for _, providerDefinition := range providerDefinitions {
		if "" == providerDefinition.Name {
			return nil, ErrProviderDefinitionNameIsRequired
		}

		if nil == providerDefinition.Provider {
			return nil, ErrProviderIsRequired
		}

		if _, exists := providerDefinitionByName[providerDefinition.Name]; true == exists {
			return nil, ErrProviderDefinitionNameMustBeUnique
		}

		providerDefinitionByName[providerDefinition.Name] = providerDefinition

		if true == providerDefinition.IsDefault {
			defaultCount = defaultCount + 1
			defaultProviderDefinitionName = providerDefinition.Name
		}
	}

	if 1 < defaultCount {
		return nil, ErrMultipleDefaultProviderDefinitions
	}

	if 0 == defaultCount {
		defaultProviderDefinitionName = providerDefinitions[0].Name
	}

	return &ManagerRegistry{
		logger:                        logger,
		providerDefinitionByName:      providerDefinitionByName,
		defaultProviderDefinitionName: defaultProviderDefinitionName,
		managers:                      make(map[string]*Manager),
	}, nil
}

func (instance *ManagerRegistry) DefaultManager() (*Manager, error) {
	return instance.Manager(instance.defaultProviderDefinitionName)
}

func (instance *ManagerRegistry) MustDefaultManager() *Manager {
	manager, managerErr := instance.DefaultManager()
	if nil != managerErr {
		exception.Panic(exception.FromError(managerErr))
	}

	return manager
}

func (instance *ManagerRegistry) DefaultDatabase() (*bun.DB, error) {
	manager, managerErr := instance.DefaultManager()
	if nil != managerErr {
		return nil, managerErr
	}

	return manager.Database(), nil
}

func (instance *ManagerRegistry) MustDefaultDatabase() *bun.DB {
	database, databaseErr := instance.DefaultDatabase()
	if nil != databaseErr {
		exception.Panic(exception.FromError(databaseErr))
	}

	return database
}

func (instance *ManagerRegistry) Manager(name string) (*Manager, error) {
	if "" == name {
		return nil, ErrProviderDefinitionNameIsRequired
	}

	instance.lock.Lock()
	defer instance.lock.Unlock()

	if manager, exists := instance.managers[name]; true == exists {
		return manager, nil
	}

	providerDefinition, exists := instance.providerDefinitionByName[name]
	if false == exists {
		return nil, ErrProviderDefinitionNotFound
	}

	database, openErr := providerDefinition.Provider.Open(providerDefinition.Params, instance.logger)
	if nil != openErr {
		return nil, openErr
	}

	manager := NewManager(name, database)
	instance.managers[name] = manager

	return manager, nil
}

func (instance *ManagerRegistry) MustManager(name string) *Manager {
	manager, managerErr := instance.Manager(name)
	if nil != managerErr {
		exception.Panic(exception.FromError(managerErr))
	}

	return manager
}

func (instance *ManagerRegistry) Database(name string) (*bun.DB, error) {
	manager, managerErr := instance.Manager(name)
	if nil != managerErr {
		return nil, managerErr
	}

	return manager.Database(), nil
}

func (instance *ManagerRegistry) MustDatabase(name string) *bun.DB {
	database, databaseErr := instance.Database(name)
	if nil != databaseErr {
		exception.Panic(exception.FromError(databaseErr))
	}

	return database
}

func (instance *ManagerRegistry) Close() error {
	instance.lock.Lock()
	defer instance.lock.Unlock()

	var closeErr error

	for _, manager := range instance.managers {
		if nil == manager {
			continue
		}

		managerCloseErr := manager.Close()
		if nil == closeErr && nil != managerCloseErr {
			closeErr = managerCloseErr
		}
	}

	return closeErr
}
