package repository

import "testing"

func TestValidateUserNamesTheFieldThatFailed(t *testing.T) {
    for expected, breakIt := range map[string]func(*testing.T) error{
        "username is required": func(t *testing.T) error {
            user := validUser()
            user.Username = "  "

            return validateUser(user)
        },
        "password hash is required": func(t *testing.T) error {
            user := validUser()
            user.Password = ""

            return validateUser(user)
        },
        "user is required": func(t *testing.T) error {
            return validateUser(nil)
        },
    } {
        t.Run(expected, func(t *testing.T) {
            err := breakIt(t)

            if nil == err {
                t.Fatalf("the user was accepted")
            }

            if expected != err.Error() {
                t.Fatalf("got %q, want %q", err.Error(), expected)
            }
        })
    }
}

/* A user with no roles is accepted here and refused at authentication instead, which is what keeps the seeded catalogue and the sign-in path from disagreeing about the same row. */
func TestValidateUserAcceptsAUserWithoutRoles(t *testing.T) {
    user := validUser()
    user.Roles = nil

    if err := validateUser(user); nil != err {
        t.Fatalf("a user without roles was refused at the repository: %v", err)
    }
}

/* This is the form the whole application compares usernames on: the service that caches by username, the three listeners that invalidate that cache and both repository implementations all fold the same way, so a lookup, a write and an invalidation address one and the same key. */
func TestNormalizedUsernameFoldsCaseAndSurroundingSpace(t *testing.T) {
    for _, spelling := range []string{"USER", " user ", "User", "\tuser\n"} {
        if "user" != NormalizedUsername(spelling) {
            t.Fatalf("%q folded to %q, not to %q", spelling, NormalizedUsername(spelling), "user")
        }
    }
}

func TestNormalizedUsernameKeepsInnerSpace(t *testing.T) {
    if "two words" != NormalizedUsername(" Two Words ") {
        t.Fatalf("unexpected fold: %q", NormalizedUsername(" Two Words "))
    }
}

func TestNextUserIdContinuesTheSeededNumbering(t *testing.T) {
    if "user-4" != nextUserId([]string{"user-3", "user-1"}) {
        t.Fatalf("unexpected identifier: %q", nextUserId([]string{"user-3", "user-1"}))
    }

    if "user-1" != nextUserId(nil) {
        t.Fatalf("an empty catalogue must start at one, got %q", nextUserId(nil))
    }
}
