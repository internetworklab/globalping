package main

import (
	pkgcli "example.com/rbmq-demo/pkg/cli"
	"github.com/alecthomas/kong"
)

var CLI struct {
	Agent pkgcli.AgentCmd `cmd:"agent"`
	Hub   pkgcli.HubCmd `cmd:"hub"`
}

func main() {
	ctx := kong.Parse(&CLI)
	err := ctx.Run()
	ctx.FatalIfErrorf(err)
}
