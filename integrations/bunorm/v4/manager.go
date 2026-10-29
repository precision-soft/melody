package bunorm

import (
    "sync"

    "github.com/uptrace/bun"
)

func NewManager(definitionName string, database *bun.DB) *Manager {
    return &Manager{
        definitionName: definitionName,
        database:       database,
    }
}

type Manager struct {
    definitionName string
    database       *bun.DB

    closeOnce sync.Once
    closeErr  error
}

func (instance *Manager) DefinitionName() string {
    return instance.definitionName
}

func (instance *Manager) Database() *bun.DB {
    return instance.database
}

func (instance *Manager) Close() error {
    instance.closeOnce.Do(
        func() {
            if nil == instance.database {
                instance.closeErr = nil
                return
            }

            instance.closeErr = instance.database.Close()
        },
    )

    return instance.closeErr
}
