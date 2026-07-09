package config

/* RoleAllowsBackgroundWork reports whether a process with the given role should start background services such as outbox relays and message consumers; the default RoleAll preserves the single-process behavior where one binary does everything. Melody itself gates nothing on the role — it is declared intent for application wiring and long-running runners to query. */
func RoleAllowsBackgroundWork(role string) bool {
    return RoleWorker == role || RoleAll == role
}

/* RoleAllowsHttp reports whether a process with the given role is meant to serve web traffic; informational — the runtime mode, not the role, decides whether the http kernel starts. */
func RoleAllowsHttp(role string) bool {
    return RoleWeb == role || RoleAll == role
}
