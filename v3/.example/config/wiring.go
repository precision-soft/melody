package config

import (
    "github.com/precision-soft/melody/v3/.example/wiring"
    melodywiring "github.com/precision-soft/melody/v3/wiring"
)

/* NewWiringBindSet is the bind set melody:wiring:generate is registered with; it is declared in the wiring package, which the generator under wiring/generate shares */
func NewWiringBindSet() *melodywiring.BindSet {
    return wiring.NewBindSet()
}
