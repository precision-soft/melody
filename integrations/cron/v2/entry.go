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
}

type RenderOptions struct {
    HeartbeatUser    string
    HeartbeatPath    string
    HeartbeatCommand []string
}
