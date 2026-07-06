package config

import (
    "fmt"
    "time"

    melodyhttp "github.com/precision-soft/melody/v3/http"
)

/* the websocket handler subscribes every /ws connection to this topic (it sets no TopicResolver), so
broadcasting here reaches every connected browser. */
const websocketDemoTopic = "default"

/* buildWebsocketDemo starts a small background ticker that broadcasts an ever-incrementing counter to
the websocket hub every 1.5s. It gives the /websocket/demo page something live to render, so the socket
fan-out is visible in the browser — not just provable in a test. The goroutine runs for the process
lifetime (this is the demo app); Broadcast is a no-op when nobody is connected. */
func (instance *Module) buildWebsocketDemo() {
    if nil == instance.serverSentEventHub {
        return
    }

    go func() {
        ticker := time.NewTicker(1500 * time.Millisecond)
        defer ticker.Stop()

        counter := 0

        for range ticker.C {
            counter++

            payload := fmt.Sprintf(`{"counter":%d,"at":%q}`, counter, time.Now().Format("15:04:05"))

            instance.serverSentEventHub.Broadcast(websocketDemoTopic, melodyhttp.ServerSentEvent{
                Event: "counter",
                Data:  payload,
            })
        }
    }()
}
