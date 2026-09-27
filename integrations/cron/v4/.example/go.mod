module github.com/precision-soft/melody/integrations/cron/v4/.example

go 1.25.0

require (
	github.com/precision-soft/melody/integrations/cron/v4 v4.0.0
	github.com/precision-soft/melody/v4 v4.0.0
)

require (
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
)

replace github.com/precision-soft/melody/v4 => ../../../../v4

replace github.com/precision-soft/melody/integrations/cron/v4 => ../
