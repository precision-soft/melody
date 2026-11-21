package version

/* buildVersion is set at build time through -ldflags. */
var buildVersion = "v4.0.0"

func BuildVersion() string {
    return buildVersion
}
