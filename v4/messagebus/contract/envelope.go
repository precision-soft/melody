package contract

type Stamp interface {
    StampName() string
}

type Envelope interface {
    Message() any

    Stamps() []Stamp

    /* WithStamp answers a copy carrying stamp after the existing ones. */
    WithStamp(stamp Stamp) Envelope

    /* WithStamps answers a copy carrying the stamps, in order, after the existing ones; nil adds none. */
    WithStamps(stamps []Stamp) Envelope
}
