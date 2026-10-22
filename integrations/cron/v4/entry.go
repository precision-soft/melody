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
    /* instance discriminator for a command expanded into several parallel runs: only InstanceCount > 1 makes the k8s template suffix the resource name with InstanceIndex, a single-instance command carries 1/1, and the crontab template ignores both */
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
