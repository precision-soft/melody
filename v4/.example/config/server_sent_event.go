package config

import (
    melodyhttp "github.com/precision-soft/melody/v4/http"
)

func (instance *Module) buildServerSentEvent() {
    instance.serverSentEventHub = melodyhttp.NewServerSentEventHub()
}
