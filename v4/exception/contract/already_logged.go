package contract

type AlreadyLogged interface {
    AlreadyLogged() bool

    MarkAsLogged()
}
