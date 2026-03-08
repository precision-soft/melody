package application

import (
    "context"
    "os"
    "os/signal"
    "syscall"
)

func NewSignalContext() (context.Context, context.CancelFunc) {
    return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
