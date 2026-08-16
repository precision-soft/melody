module github.com/precision-soft/melody/integrations/cron/v2/.example

go 1.24.9

require (
	github.com/precision-soft/melody/integrations/cron/v2 v2.1.0
	github.com/precision-soft/melody/v2 v2.10.0
)

require (
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/urfave/cli/v3 v3.6.1 // indirect
)

replace github.com/precision-soft/melody/v2 => ../../../../v2

replace github.com/precision-soft/melody/integrations/cron/v2 => ../
