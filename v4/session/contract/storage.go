package contract

import (
    "context"

    containercontract "github.com/precision-soft/melody/v4/container/contract"

    "github.com/precision-soft/melody/v4/bound"
)

/* Storage keeps sessions and is safe for concurrent use: the manager serialises Save and Delete per id only, and Load runs unlocked. Every operation but CloseWithContext takes the runtime it runs under first and refuses, in order, a nil runtime, a typed nil or one carrying no context, a closed storage, a bad id or ttl (ResolveTtl), then a done context, before the store is touched. */
type Storage interface {
    Load(runtimeInstance containercontract.Runtime, sessionId string) (map[string]any, bool, error)

    Save(runtimeInstance containercontract.Runtime, sessionId string, data map[string]any, ttl bound.Duration) error

    Delete(runtimeInstance containercontract.Runtime, sessionId string) error

    CloseWithContext(closeContext context.Context) error
}
