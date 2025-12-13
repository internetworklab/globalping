package main

import (
	pkgcli "example.com/rbmq-demo/pkg/cli"
	"github.com/alecthomas/kong"
)

var CLI struct {
	Agent pkgcli.AgentCmd
	Hub   pkgcli.HubCmd
}

func main() {
	ctx := kong.Parse(&CLI)
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
