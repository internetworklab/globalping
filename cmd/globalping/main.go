package main

import (
	"encoding/json"
	"log"
	"os"

	_ "embed"

	pkgcli "example.com/rbmq-demo/pkg/cli"
	pkgutils "example.com/rbmq-demo/pkg/utils"
	"github.com/alecthomas/kong"
	"github.com/joho/godotenv"
)

//go:embed .version.txt
var buildVersionText []byte

var CLI struct {
	Agent pkgcli.AgentCmd `cmd:"agent"`
	Hub   pkgcli.HubCmd   `cmd:"hub"`
	Bot   pkgcli.BotCmd   `cmd:"bot" help:"Serve as a Telegram bot to respond user's requests"`
}

func main() {
	if _, err := os.Stat(".env"); err == nil {
		log.Println("Loading .env file")
		err := godotenv.Load()
		if err != nil {
			log.Fatal("Error loading .env file")
		}
	}

	buildVersion, err := pkgutils.NewBuildVersion(buildVersionText)
	if err != nil && buildVersion == nil {
		log.Fatalf("failed to parse build version: %v", err)
	}
	versionJ, _ := json.Marshal(buildVersion)
	log.Printf("version: %s", string(versionJ))

	ctx := kong.Parse(&CLI)
	sharedCtx := &pkgutils.GlobalSharedContext{
		BuildVersion: buildVersion,
	}
	err = ctx.Run(sharedCtx)
	ctx.FatalIfErrorf(err)
}
