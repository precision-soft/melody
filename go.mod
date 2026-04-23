module github.com/precision-soft/melody

go 1.22

require (
	github.com/joho/godotenv v1.5.1
	github.com/urfave/cli/v3 v3.6.1
)

retract v1.10.0 // tagged on wrong commit, identical to v1.9.0; use v1.10.1
