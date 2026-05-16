module github.com/precision-soft/melody/.example

go 1.24.0

toolchain go1.24.9

require (
	github.com/precision-soft/melody v1.13.0
	github.com/precision-soft/melody/integrations/cron v1.0.0
)

require (
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
)

replace github.com/precision-soft/melody => ../

replace github.com/precision-soft/melody/integrations/cron => ../integrations/cron
