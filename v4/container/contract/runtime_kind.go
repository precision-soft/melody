package contract

/* Kind names the phase a runtime stands for. NewRequest and NewRun never answer the zero value; WithContext keeps the kind of the runtime it is given. */
type Kind uint8

const (
    KindUnspecified Kind = iota
    /* KindBoot is the application's boot: its scope and its context end when Boot returns. */
    KindBoot
    /* KindRequest is one http request. */
    KindRequest
    /* KindRun is one cli run, and every message delivery and cron tick inside it. */
    KindRun
    /* KindDetached is what a resolver answers where no resolution of a phase is in flight. */
    KindDetached
)

func (instance Kind) String() string {
    switch instance {
    case KindBoot:
        return "boot"
    case KindRequest:
        return "request"
    case KindRun:
        return "run"
    case KindDetached:
        return "detached"
    default:
        return "unspecified"
    }
}
