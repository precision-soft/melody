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
    /* instance discriminator for commands expanded into several parallel runs; InstanceCount > 1 makes the k8s template suffix the resource name with InstanceIndex so each CronJob is unique. The expansion writes 1/1 for a single-instance command — only a count above one arms the suffix — and the crontab template ignores both fields */
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
