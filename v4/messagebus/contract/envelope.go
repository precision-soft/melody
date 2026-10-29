package contract

type Stamp interface {
    StampName() string
}

type Envelope interface {
    Message() any

    Stamps() []Stamp

    WithStamp(stamps ...Stamp) Envelope
}
