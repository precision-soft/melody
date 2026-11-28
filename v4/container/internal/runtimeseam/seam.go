/* Package runtimeseam is the door package runtime reaches the container through; Token cannot be spelled outside v4/container, so the door cannot be reached by an anonymous-interface assertion. */
package runtimeseam

import (
    containercontract "github.com/precision-soft/melody/v4/container/contract"
)

type Token struct{}

/* ChildScopeSpawner opens a new scope of the container a scope belongs to: the scope of one delivery or one tick inside a run. */
type ChildScopeSpawner interface {
    SpawnChildScope(token Token) (containercontract.Scope, error)
}

type KindBinder interface {
    BindRuntimeKind(token Token, kind containercontract.Kind) error
}
