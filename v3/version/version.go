package version

/* buildVersion may be overridden at build time with -ldflags. */
var buildVersion = "v3.14.0"

func BuildVersion() string {
    return buildVersion
}
