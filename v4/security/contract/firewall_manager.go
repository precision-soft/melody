package contract

type FirewallManager interface {
    Firewall(name string) (Firewall, error)
}
