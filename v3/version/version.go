package version

/* buildVersion is set at build time through -ldflags. */
var buildVersion = "v3.14.0"

func BuildVersion() string {
    return buildVersion
}
