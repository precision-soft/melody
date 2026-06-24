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
    /* @info instance discriminator for commands expanded into several parallel runs; InstanceCount > 1 makes the k8s template suffix the resource name with InstanceIndex so each CronJob is unique. Both stay 0 for single-instance commands and are ignored by the crontab template */
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
