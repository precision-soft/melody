package contract

const (
    EventKernelRequest    = "kernel.request"
    EventKernelController = "kernel.controller"
    EventKernelResponse   = "kernel.response"
    EventKernelTerminate  = "kernel.terminate"
    EventKernelException  = "kernel.exception"
)

/* the tags the framework's own listeners carry; the application declares them on kernel.terminate and kernel.response, so those listeners still run after a listener ahead of them failed. The melody. namespace is the framework's. */
const (
    ListenerTagAccessLog = "melody.access_log"
    ListenerTagCors      = "melody.cors"
)
