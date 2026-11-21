package security

import "time"

/* defaultLeeway covers an NTP-disciplined fleet and a sloppy container clock without visibly widening a short-lived credential */
const defaultLeeway = 5 * time.Second

/* maximumLeeway keeps the tolerance from becoming a second expiry */
const maximumLeeway = 5 * time.Minute
