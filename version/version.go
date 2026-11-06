package version

/* buildVersion is overridden at build time using -ldflags */
var buildVersion = "v1.19.4"

func BuildVersion() string {
    return buildVersion
}
