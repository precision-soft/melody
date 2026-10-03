package message

/* OutboxNotice is the message enqueued through the transactional outbox: written to the outbox table in the transaction of a business change, then published by the relay at least once, with a stable id for consumer deduplication. */
type OutboxNotice struct {
    Reference string `json:"reference"`
    Text      string `json:"text"`
}
