package contract

import (
    "github.com/precision-soft/melody/v4/internal/listenertag"
)

const (
    EventKernelRequest    = "kernel.request"
    EventKernelController = "kernel.controller"
    EventKernelResponse   = "kernel.response"
    EventKernelTerminate  = "kernel.terminate"
    EventKernelException  = "kernel.exception"
)

/* the tags the framework's own listeners carry; the application declares them on kernel.terminate and kernel.response, so those listeners still run after a listener ahead of them failed. The melody. namespace is the framework's. */
const (
    ListenerTagKernelAccessLog = listenertag.FrameworkAccessLog
    ListenerTagKernelCors      = listenertag.FrameworkCors
    ListenerTagKernelSecurityHeaders = listenertag.FrameworkSecurityHeaders
)
