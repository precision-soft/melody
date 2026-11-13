package message

import (
    "errors"
)

/* ErrTransportNotConfigured answers a door that would publish to a broker this process was not given: without AMQP_DSN a message would wait in a process-local queue that nothing in an http process reads, so the doors refuse it by name instead */
var ErrTransportNotConfigured = errors.New("no message transport is configured; set AMQP_DSN")
