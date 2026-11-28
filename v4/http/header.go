package http

const (
    HeaderRequestId = "X-Request-Id"
)

/* the security headers the framework sends, in their canonical spelling */
const (
    HeaderContentTypeOptions        = "X-Content-Type-Options"
    HeaderFrameOptions              = "X-Frame-Options"
    HeaderReferrerPolicy            = "Referrer-Policy"
    HeaderStrictTransportSecurity   = "Strict-Transport-Security"
    HeaderContentSecurityPolicy     = "Content-Security-Policy"
    HeaderCrossOriginOpenerPolicy   = "Cross-Origin-Opener-Policy"
    HeaderCrossOriginResourcePolicy = "Cross-Origin-Resource-Policy"
    HeaderPermissionsPolicy         = "Permissions-Policy"
)

/* SecurityHeaderNames answers the names of the security headers the framework sends, a list of the caller's own. */
func SecurityHeaderNames() []string {
    return []string{
        HeaderContentTypeOptions,
        HeaderFrameOptions,
        HeaderReferrerPolicy,
        HeaderStrictTransportSecurity,
        HeaderContentSecurityPolicy,
        HeaderCrossOriginOpenerPolicy,
        HeaderCrossOriginResourcePolicy,
        HeaderPermissionsPolicy,
    }
}
