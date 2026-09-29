package main

import (
    "context"
    "encoding/json"
    "fmt"
    "strings"
    "time"

    outbox "github.com/precision-soft/melody/integrations/outbox/v3"
    melodymessagebus "github.com/precision-soft/melody/v3/messagebus"
    messagebuscontract "github.com/precision-soft/melody/v3/messagebus/contract"
    runtimecontract "github.com/precision-soft/melody/v3/runtime/contract"
    "github.com/uptrace/bun"
)

type orderMessage struct {
    Order string `json:"order"`
}

/* poisonMessage encodes fine but is deliberately undecodable, so the relay must dead-letter it as poison rather than retry it forever. */
type poisonMessage struct{}

/* e2eCodec is the outbox MessageCodec: it round-trips orderMessage and deliberately fails to decode the poison type so the poison-dead-letter path can be observed. */
type e2eCodec struct{}

func (instance *e2eCodec) Encode(message any) (string, []byte, error) {
    switch typed := message.(type) {
    case orderMessage:
        payload, marshalErr := json.Marshal(typed)

        return "order", payload, marshalErr
    case poisonMessage:
        return "poison", []byte("{}"), nil
    default:
        return "", nil, fmt.Errorf("e2e codec: unknown message type %T", message)
    }
}

func (instance *e2eCodec) Decode(typeName string, payload []byte) (any, error) {
    switch typeName {
    case "order":
        var order orderMessage
        if unmarshalErr := json.Unmarshal(payload, &order); nil != unmarshalErr {
            return nil, unmarshalErr
        }

        return order, nil
    case "poison":
        return nil, fmt.Errorf("poison: undecodable by design")
    default:
        return nil, fmt.Errorf("e2e codec: unknown type %q", typeName)
    }
}

/* recordingTransport is a messagebuscontract.Transport that only records the message ids it is asked to send, so the harness can assert the relay published each outbox row under its stable, deduplicable id. */
type recordingTransport struct {
    sent []string
}

func (instance *recordingTransport) Send(_ runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    if messageId, hasMessageId := melodymessagebus.MessageId(envelope); true == hasMessageId {
        instance.sent = append(instance.sent, messageId)
    }

    return nil
}

func (instance *recordingTransport) Receive(_ runtimecontract.Runtime) (<-chan messagebuscontract.Envelope, error) {
    return nil, nil
}

func (instance *recordingTransport) Ack(_ runtimecontract.Runtime, _ messagebuscontract.Envelope) error {
    return nil
}

func (instance *recordingTransport) Nack(_ runtimecontract.Runtime, _ messagebuscontract.Envelope, _ bool) error {
    return nil
}

func (instance *recordingTransport) Close() error {
    return nil
}

/* runOutboxCheck exercises the transactional-outbox guarantee against a live postgres: business rows are enqueued in a transaction, then the relay claims the batch (FOR UPDATE SKIP LOCKED), publishes each row to the transport under a stable id, marks it sent, and dead-letters an undecodable (poison) row instead of retrying it forever. */
func runOutboxCheck(dsn string) {
    ctx := context.Background()

    database := openPostgres(dsn)
    defer database.Close()

    codec := &e2eCodec{}
    store := outbox.NewStore(database, codec)

    if schemaErr := store.EnsureSchema(ctx); nil != schemaErr {
        fail("outbox ensure schema: %v", schemaErr)
    }

    /* start from a clean table so the published/dead counts are deterministic across runs */
    if _, truncateErr := database.ExecContext(ctx, "DELETE FROM melody_outbox"); nil != truncateErr {
        fail("outbox truncate: %v", truncateErr)
    }

    /* two good business messages + one poison, each enqueued transactionally (here each in its own tx — in real use the outbox write shares the business write's tx) */
    for _, order := range []string{"SO-1", "SO-2"} {
        if enqueueErr := store.Enqueue(ctx, database, orderMessage{Order: order}); nil != enqueueErr {
            fail("outbox enqueue %s: %v", order, enqueueErr)
        }
    }
    if enqueueErr := store.Enqueue(ctx, database, poisonMessage{}); nil != enqueueErr {
        fail("outbox enqueue poison: %v", enqueueErr)
    }
    pass("enqueued 2 orders + 1 poison row (transactional)")

    transport := &recordingTransport{}
    relay := outbox.NewRelay(outbox.RelayConfig{
        Repository: store,
        Transport:  transport,
        Codec:      codec,
        BatchSize:  10,
    })

    published, runErr := relay.RunOnce(newRuntime())
    if nil != runErr {
        fail("outbox relay run: %v", runErr)
    }
    if 2 != published {
        fail("expected 2 good rows published, got %d", published)
    }
    if 2 != len(transport.sent) {
        fail("expected the transport to receive 2 messages, got %d (%v)", len(transport.sent), transport.sent)
    }
    pass("relay published 2 rows to the transport under stable ids %v (poison NOT published)", transport.sent)

    /* the poison row must have been dead-lettered, not left pending; a second run finds nothing due */
    republished, secondErr := relay.RunOnce(newRuntime())
    if nil != secondErr {
        fail("outbox second relay run: %v", secondErr)
    }
    if 0 != republished {
        fail("expected 0 rows due on the second run (all sent/dead), got %d", republished)
    }

    deadCount := countOutboxByStatus(ctx, dsn, "dead")
    sentCount := countOutboxByStatus(ctx, dsn, "sent")
    if 1 != deadCount {
        fail("expected exactly 1 dead (poison) row, got %d", deadCount)
    }
    pass("second run published 0 (idempotent); final state: sent=%d dead=%d (poison dead-lettered)", sentCount, deadCount)
    assertOutboxRetriesARefusedSendAfterItsBackoff(ctx, dsn, database, store, codec)
}

/* refusingOnceTransport refuses the first send it is asked for and records every later one, the shape of a broker that was briefly unreachable. */
type refusingOnceTransport struct {
    recordingTransport
    refused bool
}

func (instance *refusingOnceTransport) Send(runtimeInstance runtimecontract.Runtime, envelope messagebuscontract.Envelope) error {
    if false == instance.refused {
        instance.refused = true

        return fmt.Errorf("e2e transport: refused once")
    }

    return instance.recordingTransport.Send(runtimeInstance, envelope)
}

/* a send the transport refuses is not lost and not retried at once: the row stays pending with the attempt counted and the error kept, its next claim pushed past the backoff, read out of band as the store holds it; a run before the backoff finds nothing due, and the first run after it publishes the row and marks it sent */
func assertOutboxRetriesARefusedSendAfterItsBackoff(ctx context.Context, dsn string, database *bun.DB, store *outbox.Store, codec *e2eCodec) {
    const initialBackoff = 2 * time.Second

    if enqueueErr := store.Enqueue(ctx, database, orderMessage{Order: "SO-RETRY"}); nil != enqueueErr {
        fail("outbox enqueue the retry probe: %v", enqueueErr)
    }

    transport := &refusingOnceTransport{}
    relay := outbox.NewRelay(outbox.RelayConfig{
        Repository:     store,
        Transport:      transport,
        Codec:          codec,
        BatchSize:      10,
        InitialBackoff: initialBackoff,
    })

    refusedRun, refusedErr := relay.RunOnce(newRuntime())
    if nil != refusedErr || 0 != refusedRun {
        fail("outbox: the run whose send was refused published %d (err %v), wanted 0", refusedRun, refusedErr)
    }

    status := ""
    attempts := 0
    lastError := ""
    secondsUntilDue := 0.0
    row := database.QueryRowContext(ctx, "SELECT status, attempts, COALESCE(last_error, ''), EXTRACT(EPOCH FROM (available_at - NOW() AT TIME ZONE 'UTC')) FROM melody_outbox WHERE status <> 'dead' AND status <> 'sent'")
    if scanErr := row.Scan(&status, &attempts, &lastError, &secondsUntilDue); nil != scanErr {
        fail("outbox: read the refused row out of band: %v", scanErr)
    }
    if "pending" != status || 1 != attempts || false == strings.Contains(lastError, "refused once") || 0.5 > secondsUntilDue || initialBackoff.Seconds()+1 < secondsUntilDue {
        fail("outbox: the refused row reads status %q, attempts %d, last error %q, due in %.2fs — wanted pending, 1, the refusal, and a due time inside the %s backoff", status, attempts, lastError, secondsUntilDue, initialBackoff)
    }

    earlyRun, earlyErr := relay.RunOnce(newRuntime())
    if nil != earlyErr || 0 != earlyRun {
        fail("outbox: a run inside the backoff published %d (err %v), wanted 0 — the refused row was claimed before it was due", earlyRun, earlyErr)
    }

    time.Sleep(initialBackoff + 500*time.Millisecond)

    dueRun, dueErr := relay.RunOnce(newRuntime())
    if nil != dueErr || 1 != dueRun || 1 != len(transport.sent) {
        fail("outbox: the run after the backoff published %d (err %v, sent %v), wanted the refused row once", dueRun, dueErr, transport.sent)
    }
    if 3 != countOutboxByStatus(ctx, dsn, "sent") {
        fail("outbox: after the retry the table holds %d sent rows, wanted the two orders and the retried one", countOutboxByStatus(ctx, dsn, "sent"))
    }
    pass("a refused send left the row pending with attempts 1 and its error, due %.1fs later; a run inside the backoff published nothing and the run after it published the row once (read out of band)", secondsUntilDue)
}

func countOutboxByStatus(ctx context.Context, dsn string, status string) int {
    database := openPostgres(dsn)
    defer database.Close()

    count := 0
    row := database.QueryRowContext(ctx, "SELECT COUNT(*) FROM melody_outbox WHERE status = ?", status)
    if scanErr := row.Scan(&count); nil != scanErr {
        fail("outbox count %s: %v", status, scanErr)
    }

    return count
}
