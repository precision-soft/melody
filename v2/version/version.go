package version

/* buildVersion is overridden at build time using -ldflags */
var buildVersion = "v2.13.4"

func BuildVersion() string {
    return buildVersion
}
