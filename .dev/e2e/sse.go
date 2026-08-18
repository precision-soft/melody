package main

import (
    "bufio"
    "context"
    "crypto/rand"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "strings"
    "time"

    melodyrueidis "github.com/precision-soft/melody/integrations/rueidis/v3"
    melodyhttp "github.com/precision-soft/melody/v3/http"
    "github.com/redis/rueidis"
)

const serverSentEventLabel = "server-sent events"

const (
    serverSentEventStreamRoute  = "/events/stream/"
    serverSentEventPublishRoute = "/events/publish/"
)

/* serverSentEventBackplaneChannel is defaultServerSentEventBackplaneChannel in the rueidis integration. It is
unexported there, so it is mirrored here — and the wire event below is a mirror of that package's own
serverSentEventWireEvent for the same reason. A rename on either side surfaces as a failed delivery in the very
first wire assertion rather than as silence. */
const serverSentEventBackplaneChannel = "melody:sse"

/* serverSentEventWirePayload is the json the backplane publishes: the ORIGIN of the replica that produced the
event (which is how a replica recognises and drops its own), the topic, and the event itself. ServerSentEvent
carries no json tags, so its fields travel under their Go names. */
type serverSentEventWirePayload struct {
    Origin string                     `json:"origin"`
    Topic  string                     `json:"topic"`
    Event  melodyhttp.ServerSentEvent `json:"event"`
}

/* serverSentEventArrivalBudget is how long an assertion waits for a frame that MUST arrive. */
const serverSentEventArrivalBudget = 8 * time.Second

/* serverSentEventSilenceBudget is how long an assertion waits to be sure a frame must NOT arrive. It is
deliberately shorter: every path being probed for absence (a suppressed origin, another topic, a split frame) would
deliver immediately if it were broken, so a longer wait buys nothing and costs the run three times over. */
const serverSentEventSilenceBudget = 2 * time.Second

/* runServerSentEventCheck drives the live event stream of the supervised application while acting as a SECOND
REPLICA on the same redis backplane. That is what makes the cross-replica story reachable without changing the
example: the example already wires the rueidis backplane (config/redis.go), so an event published on the shared
channel by anyone reaches every subscriber of every replica.

The properties covered, and what each one catches:

  - the preamble is flushed BEFORE any event, and the response carries the event-stream headers. A stream whose
    headers or preamble are buffered looks alive to the server and dead to every client behind a proxy;
  - an in-process publish reaches the live stream (the /events/publish route dispatches through the message bus,
    whose handler broadcasts on the hub);
  - a WIRE event the harness publishes on the redis channel reaches the stream. This is also the only way to
    exercise the id: and retry: framing at all, because the example never sets an event id;
  - ID SANITISATION: an id carrying a blank line must not split the frame. Unsanitised, it would end the current
    frame and start another one whose content the publisher chose — response splitting, inside the one framing the
    framework fully controls;
  - ORIGIN SUPPRESSION: a replica's own broadcast must reach its own subscriber exactly ONCE. Without the origin
    check every broadcast is delivered twice on the originating replica — locally, then again off the channel;
  - TOPIC ISOLATION: an event on another topic reaches nothing here;
  - a MALFORMED payload on the shared channel must not kill the subscription. One bad publisher would otherwise
    silently deafen every replica until it restarts, and nothing in the http surface reveals it.

Cleanup matters more here than anywhere else in the harness: an abandoned stream request leaves a subscriber and a
goroutine inside the LONG-LIVED supervised application, which no later run can clean up. The streaming body is
therefore closed through pushFailureCleanup as well as on the happy path. */
func runServerSentEventCheck(baseUrl string, redisAddress string) {
    client := newLiveExampleClient(baseUrl)

    redisClient := openRedis(redisAddress)
    defer redisClient.Close()

    topic := liveExampleUnique("e2e-sse")

    response := client.openStream(serverSentEventLabel, serverSentEventStreamRoute+"?topic="+topic, map[string]string{
        "Accept": "text/event-stream",
    })

    /* both paths, because os.Exit runs no deferred function: without the failure hook a red assertion below leaves
       this stream open and its subscriber registered inside the supervised application forever */
    releaseStream := pushFailureCleanup(func() {
        _ = response.Body.Close()
    })
    defer func() {
        releaseStream()
        _ = response.Body.Close()
    }()

    assertServerSentEventHeaders(response)

    reader := newServerSentEventStreamReader(response.Body)

    assertServerSentEventPreamble(reader)
    assertServerSentEventInProcessPublish(client, reader, topic)
    assertServerSentEventWireDelivery(redisClient, reader, topic)
    assertServerSentEventIdSanitisation(redisClient, reader, topic)
    assertServerSentEventTopicIsolation(redisClient, reader, topic)
    assertServerSentEventMalformedPayloadSurvives(redisClient, reader, topic)
    assertServerSentEventOriginSuppression(redisAddress, redisClient)
}

func assertServerSentEventHeaders(response *http.Response) {
    if http.StatusOK != response.StatusCode {
        fail("%s: the stream request answered %d, wanted 200", serverSentEventLabel, response.StatusCode)
    }

    for name, expected := range map[string]string{
        "Content-Type":  "text/event-stream",
        "Cache-Control": "no-cache",
        /* the proxy-buffering opt-out is part of the contract, not a nicety: behind nginx a stream without it is
           held in the proxy buffer and the client sees nothing until the response ends, which for a stream is
           never */
        "X-Accel-Buffering": "no",
    } {
        actual := response.Header.Get(name)
        if false == strings.Contains(actual, expected) {
            fail("%s: the stream response carries %s: %q, wanted %q", serverSentEventLabel, name, actual, expected)
        }
    }

    pass("the stream answered 200 with text/event-stream, no-cache and the proxy-buffering opt-out")
}

/* assertServerSentEventPreamble asserts the ordering the handler promises: the comment is flushed at subscription
time, so it arrives BEFORE anything is published. Nothing has been published at this point in the section, so an
arriving frame that is not the comment means the flush was deferred until the first event — and a client that waits
for the preamble to know it is connected would then hang until something happened to be broadcast. */
func assertServerSentEventPreamble(reader *serverSentEventStreamReader) {
    frame, arrived := reader.next(serverSentEventArrivalBudget)
    if false == arrived {
        fail(
            "%s: no preamble arrived within %s — the handler's comment was not flushed at subscription time, so a client cannot tell a live stream from a hung one",
            serverSentEventLabel,
            serverSentEventArrivalBudget,
        )
    }

    if false == frame.isComment() {
        fail(
            "%s: the first frame on the stream is not the preamble comment but %s — something was published before the stream announced itself",
            serverSentEventLabel,
            frame.describe(),
        )
    }
    if false == strings.Contains(strings.Join(frame.commentList, " "), "connected") {
        fail("%s: the preamble comment is %q, wanted the handler's \"connected\"", serverSentEventLabel, strings.Join(frame.commentList, " "))
    }

    pass("the preamble comment was flushed before any event was published")
}

func assertServerSentEventInProcessPublish(client *liveExampleClient, reader *serverSentEventStreamReader, topic string) {
    text := liveExampleUnique("in-process")

    path := fmt.Sprintf("%s?topic=%s&text=%s", serverSentEventPublishRoute, topic, text)

    response := client.get(serverSentEventLabel, path)
    requireLiveExampleStatus(serverSentEventLabel, path, response, http.StatusAccepted)

    frame := requireServerSentEventFrame(reader, "the in-process publish")

    if text != frame.data() {
        fail(
            "%s: the in-process publish delivered %q, wanted %q (%s)",
            serverSentEventLabel,
            frame.data(),
            text,
            frame.describe(),
        )
    }
    if "notification" != frame.event {
        fail("%s: the in-process publish arrived with event %q, wanted \"notification\"", serverSentEventLabel, frame.event)
    }

    pass("an in-process publish reached the live stream as event %q", frame.event)
}

/* assertServerSentEventWireDelivery publishes straight onto the shared channel under a FOREIGN origin, which is
what a second replica's broadcast looks like from this application's side. It is also the only place the id: and
retry: fields are exercised, since nothing in the example ever sets them. */
func assertServerSentEventWireDelivery(redisClient rueidis.Client, reader *serverSentEventStreamReader, topic string) {
    data := liveExampleUnique("off-the-wire")
    eventId := liveExampleUnique("event-id")

    publishServerSentEventWirePayload(redisClient, serverSentEventWirePayload{
        Origin: newServerSentEventForeignOrigin(),
        Topic:  topic,
        Event: melodyhttp.ServerSentEvent{
            Id:    eventId,
            Event: "wire",
            Data:  data,
            Retry: 7000,
        },
    })

    frame := requireServerSentEventFrame(reader, "the wire event from another replica")

    if data != frame.data() {
        fail("%s: the wire event delivered %q, wanted %q (%s)", serverSentEventLabel, frame.data(), data, frame.describe())
    }
    if eventId != frame.id {
        fail("%s: the wire event arrived with id %q, wanted %q — the id field was dropped in framing", serverSentEventLabel, frame.id, eventId)
    }
    if "7000" != frame.retry {
        fail("%s: the wire event arrived with retry %q, wanted \"7000\"", serverSentEventLabel, frame.retry)
    }
    if "wire" != frame.event {
        fail("%s: the wire event arrived with event %q, wanted \"wire\"", serverSentEventLabel, frame.event)
    }

    pass("an event published on the redis channel by another replica reached the stream framed with id: and retry:")
}

/* assertServerSentEventIdSanitisation is the response-splitting probe. The id carries a BLANK LINE, which in the
event-stream grammar terminates a frame: unsanitised, the writer emits the id, then the blank line ends the frame
early, and the rest of the id is read as the beginning of a NEW frame whose fields the publisher chose. The
assertions are therefore that exactly one frame arrives, that it carries the real data, and that nothing carrying
the injected content follows it. */
func assertServerSentEventIdSanitisation(redisClient rueidis.Client, reader *serverSentEventStreamReader, topic string) {
    data := liveExampleUnique("un-split")
    injected := liveExampleUnique("injected-frame")

    publishServerSentEventWirePayload(redisClient, serverSentEventWirePayload{
        Origin: newServerSentEventForeignOrigin(),
        Topic:  topic,
        Event: melodyhttp.ServerSentEvent{
            Id:    "before\n\ndata: " + injected,
            Event: "sanitised",
            Data:  data,
        },
    })

    frame := requireServerSentEventFrame(reader, "the event carrying a newline in its id")

    if "before" == frame.id {
        fail(
            "%s: the id was written verbatim, so the blank line inside it ended the frame early — a publisher can inject whole frames into the stream (response splitting)",
            serverSentEventLabel,
        )
    }
    if true == strings.Contains(frame.id, "\n") || true == strings.Contains(frame.id, "\r") {
        fail("%s: the delivered id still holds a line break: %q", serverSentEventLabel, frame.id)
    }
    if data != frame.data() {
        fail(
            "%s: the frame carrying the tampered id delivered %q, wanted the real payload %q (%s)",
            serverSentEventLabel,
            frame.data(),
            data,
            frame.describe(),
        )
    }
    if true == strings.Contains(frame.data(), injected) {
        fail("%s: the injected content arrived as data inside the frame: %q", serverSentEventLabel, frame.data())
    }

    /* the split would produce a SECOND frame immediately, so its absence is the other half of the assertion */
    if extra, arrived := reader.next(serverSentEventSilenceBudget); true == arrived {
        fail(
            "%s: a second frame followed the sanitised one (%s) — the id split the stream into two frames",
            serverSentEventLabel,
            extra.describe(),
        )
    }

    pass("an id carrying a blank line was sanitised into one frame instead of splitting the stream")
}

func assertServerSentEventTopicIsolation(redisClient rueidis.Client, reader *serverSentEventStreamReader, topic string) {
    otherTopic := topic + "-elsewhere"

    publishServerSentEventWirePayload(redisClient, serverSentEventWirePayload{
        Origin: newServerSentEventForeignOrigin(),
        Topic:  otherTopic,
        Event:  melodyhttp.ServerSentEvent{Data: liveExampleUnique("wrong-topic")},
    })

    if frame, arrived := reader.next(serverSentEventSilenceBudget); true == arrived {
        fail(
            "%s: an event published on %q was delivered to a subscriber of %q (%s) — every stream would receive every other stream's events",
            serverSentEventLabel,
            otherTopic,
            topic,
            frame.describe(),
        )
    }

    pass("an event published on another topic reached nothing on this stream")
}

/* assertServerSentEventMalformedPayloadSurvives publishes a payload the backplane cannot decode and then a valid
one. The valid event arriving is the assertion: a decoder that returned from its subscription callback in a way that
tore down the subscription would leave the replica permanently deaf to the channel, and nothing on the http surface
would reveal it — the stream stays open, the headers stay correct, and events simply stop. */
func assertServerSentEventMalformedPayloadSurvives(redisClient rueidis.Client, reader *serverSentEventStreamReader, topic string) {
    publishServerSentEventRaw(redisClient, "this is not a json wire event at all")
    publishServerSentEventRaw(redisClient, `{"origin":"x","topic":`)

    data := liveExampleUnique("after-the-garbage")

    publishServerSentEventWirePayload(redisClient, serverSentEventWirePayload{
        Origin: newServerSentEventForeignOrigin(),
        Topic:  topic,
        Event:  melodyhttp.ServerSentEvent{Data: data},
    })

    frame := requireServerSentEventFrame(reader, "the valid event that followed two malformed payloads")

    if data != frame.data() {
        fail(
            "%s: the event after the malformed payloads delivered %q, wanted %q (%s)",
            serverSentEventLabel,
            frame.data(),
            data,
            frame.describe(),
        )
    }

    pass("two malformed payloads on the shared channel did not deafen the subscription (the next valid event arrived)")
}

/* assertServerSentEventOriginSuppression stands the harness up as a full replica — its own hub, its own backplane
on the same channel — and broadcasts through it. The event must reach the harness's own subscriber exactly once:
locally, and NOT a second time off the channel it just published to.

The check begins by proving the harness's own subscription is live, with a foreign-origin publish. Without that
guard a backplane that had not finished subscribing would deliver the broadcast once for the trivial reason that it
never heard its own message back — the assertion would be green and would have tested nothing. */
func assertServerSentEventOriginSuppression(redisAddress string, publishClient rueidis.Client) {
    hub := melodyhttp.NewServerSentEventHub()

    replicaClient := openRedis(redisAddress)

    backplane := melodyrueidis.NewServerSentEventBackplane(replicaClient, hub)

    releaseReplica := pushFailureCleanup(func() {
        _ = backplane.Close()
        hub.Shutdown()
        replicaClient.Close()
    })
    defer func() {
        releaseReplica()
        _ = backplane.Close()
        hub.Shutdown()
        replicaClient.Close()
    }()

    topic := liveExampleUnique("e2e-sse-origin")
    subscriber := hub.Subscribe(topic, 16)
    defer hub.Unsubscribe(subscriber)

    /* non-vacuity guard: a foreign-origin publish must reach the subscriber before the broadcast below means
       anything at all. The publish is repeated because a redis SUBSCRIBE is established asynchronously and a
       message published before it lands is simply gone — pub/sub keeps nothing for a subscriber that was not
       there yet. */
    probe := liveExampleUnique("subscription-probe")
    subscriptionLive := false

    for attempt := 1; attempt <= 40 && false == subscriptionLive; attempt++ {
        publishServerSentEventWirePayload(publishClient, serverSentEventWirePayload{
            Origin: newServerSentEventForeignOrigin(),
            Topic:  topic,
            Event:  melodyhttp.ServerSentEvent{Data: probe},
        })

        select {
        case event := <-subscriber.Events():
            if probe != event.Data {
                fail("%s: the subscription probe delivered %q, wanted %q", serverSentEventLabel, event.Data, probe)
            }

            subscriptionLive = true
        case <-time.After(250 * time.Millisecond):
        }
    }

    if false == subscriptionLive {
        fail(
            "%s: the harness replica's backplane never received a foreign-origin event, so the origin-suppression assertion below would pass for the wrong reason",
            serverSentEventLabel,
        )
    }

    /* drain anything the retry loop may have delivered more than once before the measurement starts */
    for drained := true; true == drained; {
        select {
        case <-subscriber.Events():
        default:
            drained = false
        }
    }

    broadcast := liveExampleUnique("own-broadcast")
    delivered := hub.Broadcast(topic, melodyhttp.ServerSentEvent{Data: broadcast})
    if 1 != delivered {
        fail("%s: the replica's own broadcast reached %d local subscriber(s), wanted 1", serverSentEventLabel, delivered)
    }

    select {
    case event := <-subscriber.Events():
        if broadcast != event.Data {
            fail("%s: the replica's own broadcast delivered %q, wanted %q", serverSentEventLabel, event.Data, broadcast)
        }
    case <-time.After(serverSentEventArrivalBudget):
        fail("%s: the replica's own broadcast never reached its own subscriber", serverSentEventLabel)
    }

    select {
    case event := <-subscriber.Events():
        fail(
            "%s: the replica's own broadcast was delivered TWICE (second copy %q) — the origin check did not drop the replica's own message coming back off the channel, so every broadcast is duplicated on the node that made it",
            serverSentEventLabel,
            event.Data,
        )
    case <-time.After(serverSentEventSilenceBudget):
    }

    pass("a replica's own broadcast reached its own subscriber exactly once (its echo off the channel was suppressed by origin)")
}

func publishServerSentEventWirePayload(client rueidis.Client, payload serverSentEventWirePayload) {
    encoded, marshalErr := json.Marshal(payload)
    if nil != marshalErr {
        fail("%s: encode the wire event: %v", serverSentEventLabel, marshalErr)
    }

    publishServerSentEventRaw(client, string(encoded))
}

func publishServerSentEventRaw(client rueidis.Client, message string) {
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    result := client.Do(ctx, client.B().Publish().Channel(serverSentEventBackplaneChannel).Message(message).Build())
    if resultErr := result.Error(); nil != resultErr {
        fail("%s: publish on %q: %v", serverSentEventLabel, serverSentEventBackplaneChannel, resultErr)
    }
}

/* newServerSentEventForeignOrigin produces an origin no replica can be using: the backplane derives its own from
16 random bytes, so a random 16-byte value collides with a live replica's origin with probability zero for any run
that ever finishes. An origin that DID collide would make the application drop the event as its own and the
assertion would report a delivery failure. */
func newServerSentEventForeignOrigin() string {
    buffer := make([]byte, 16)
    if _, readErr := rand.Read(buffer); nil != readErr {
        fail("%s: generate a foreign backplane origin: %v", serverSentEventLabel, readErr)
    }

    return hex.EncodeToString(buffer)
}

func requireServerSentEventFrame(reader *serverSentEventStreamReader, what string) serverSentEventFrame {
    frame, arrived := reader.next(serverSentEventArrivalBudget)
    if false == arrived {
        fail("%s: %s never reached the stream within %s", serverSentEventLabel, what, serverSentEventArrivalBudget)
    }

    /* a keepalive comment can legitimately interleave with the events, so a comment frame is stepped over rather
       than mistaken for the event under test */
    for true == frame.isComment() {
        frame, arrived = reader.next(serverSentEventArrivalBudget)
        if false == arrived {
            fail("%s: %s never reached the stream within %s (only comments arrived)", serverSentEventLabel, what, serverSentEventArrivalBudget)
        }
    }

    return frame
}

/* serverSentEventFrame is one parsed frame. The field count is kept so a comment-only frame is distinguishable
from a frame that merely happens to carry no data — those are different things on the wire, and the preamble
assertion turns on the difference. */
type serverSentEventFrame struct {
    commentList []string
    unknownList []string
    id          string
    event       string
    retry       string
    dataList    []string
    fieldCount  int
}

func (instance serverSentEventFrame) data() string {
    return strings.Join(instance.dataList, "\n")
}

func (instance serverSentEventFrame) isComment() bool {
    return 0 < len(instance.commentList) && 0 == instance.fieldCount
}

func (instance serverSentEventFrame) describe() string {
    return fmt.Sprintf(
        "frame{id=%q event=%q retry=%q data=%q comments=%v unknown=%v}",
        instance.id,
        instance.event,
        instance.retry,
        instance.data(),
        instance.commentList,
        instance.unknownList,
    )
}

/* parseServerSentEventFrame turns the lines of one frame into fields, following the event-stream grammar: a line
whose first character is a colon is a comment, otherwise the name is everything before the first colon and the
value everything after it with ONE optional leading space removed. A line with no colon at all is a field with an
empty value. */
func parseServerSentEventFrame(lineList []string) serverSentEventFrame {
    frame := serverSentEventFrame{}

    for _, line := range lineList {
        name, value := splitServerSentEventLine(line)

        if "" == name {
            frame.commentList = append(frame.commentList, value)

            continue
        }

        frame.fieldCount++

        switch name {
        case "id":
            frame.id = value
        case "event":
            frame.event = value
        case "retry":
            frame.retry = value
        case "data":
            frame.dataList = append(frame.dataList, value)
        default:
            frame.unknownList = append(frame.unknownList, name)
        }
    }

    return frame
}

func splitServerSentEventLine(line string) (string, string) {
    colon := strings.Index(line, ":")
    if -1 == colon {
        return line, ""
    }

    value := line[colon+1:]
    if true == strings.HasPrefix(value, " ") {
        value = value[1:]
    }

    return line[:colon], value
}

/* serverSentEventStreamReader turns a live stream body into a channel of frames. A goroutine is unavoidable: the
body never ends, so every read has to be able to outlive the assertion that is waiting for it, and closing the body
is what stops the goroutine. */
type serverSentEventStreamReader struct {
    frameList chan serverSentEventFrame
}

func newServerSentEventStreamReader(body io.Reader) *serverSentEventStreamReader {
    instance := &serverSentEventStreamReader{
        frameList: make(chan serverSentEventFrame, 64),
    }

    go func() {
        defer close(instance.frameList)

        reader := bufio.NewReader(body)
        blockList := []string{}

        for {
            line, readErr := reader.ReadString('\n')

            if 0 < len(line) {
                trimmed := strings.TrimRight(line, "\r\n")

                if "" == trimmed {
                    if 0 < len(blockList) {
                        instance.frameList <- parseServerSentEventFrame(blockList)
                        blockList = nil
                    }
                } else {
                    blockList = append(blockList, trimmed)
                }
            }

            if nil != readErr {
                return
            }
        }
    }()

    return instance
}

/* next waits up to budget for the next frame. The second return value distinguishes "no frame arrived" from a
frame whose fields are all empty — the absence assertions turn on exactly that difference. */
func (instance *serverSentEventStreamReader) next(budget time.Duration) (serverSentEventFrame, bool) {
    select {
    case frame, open := <-instance.frameList:
        if false == open {
            return serverSentEventFrame{}, false
        }

        return frame, true
    case <-time.After(budget):
        return serverSentEventFrame{}, false
    }
}
