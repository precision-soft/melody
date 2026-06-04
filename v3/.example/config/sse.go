package config

import (
    melodyhttp "github.com/precision-soft/melody/v3/http"
)

func (instance *Module) buildSse() {
    instance.sseHub = melodyhttp.NewSseHub()
}
