package cron

type Entry struct {
    Name            string
    User            string
    Binary          string
    Args            []string
    Schedule        *Schedule
    Command         []string
    LogPath         string
    DestinationFile string
    /* InstanceIndex distinguishes expanded parallel runs. Kubernetes suffixes resource names only when InstanceCount exceeds one; single-instance expansion uses 1/1, and crontab ignores these fields. */
    InstanceIndex int
    InstanceCount int
}

type RenderOptions struct {
    HeartbeatUser    string
    HeartbeatPath    string
    HeartbeatCommand []string
    Image            string
    Namespace        string
    RestartPolicy    string
}
