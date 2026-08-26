package message

/* OutboxNotice is the message enqueued through the transactional outbox: it is written to the outbox table in the same transaction as a business change, then the relay publishes it to the message transport exactly-once-ish (at-least-once with a stable id for consumer deduplication). */
type OutboxNotice struct {
    Reference string `json:"reference"`
    Text      string `json:"text"`
}
