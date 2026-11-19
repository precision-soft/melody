package contract

type Stamp interface {
    StampName() string
}

type Envelope interface {
    Message() any

    Stamps() []Stamp

    /* WithStamps answers a copy carrying the stamps, in order, after the existing ones; nil adds none. */
    WithStamps(stamps []Stamp) Envelope
}
