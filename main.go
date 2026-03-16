package main

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/anthropics/docs7/api"
	"github.com/anthropics/docs7/cmd"
)

var cli struct {
	Search cmd.SearchCmd `cmd:"" help:"Find a library by name"`
	Docs   cmd.DocsCmd   `cmd:"" help:"Get documentation source URLs for a library"`
	Read   cmd.ReadCmd   `cmd:"" help:"Read a document URL (github:// or https://)"`
	Auth   cmd.AuthCmd   `cmd:"" help:"Manage authentication for docs7 providers"`

	Login  cmd.LoginCmd  `cmd:"" hidden:"" help:"Log in to Context7 (deprecated: use 'auth ctx7')"`
	Logout cmd.LogoutCmd `cmd:"" hidden:"" help:"Log out (deprecated: use 'auth logout')"`
	Whoami cmd.WhoamiCmd `cmd:"" hidden:"" help:"Show login status (deprecated: use 'auth status')"`
}

func main() {
	ctx := kong.Parse(&cli,
		kong.Name("docs7"),
		kong.Description("Library documentation finder — ctx7 index + full document URLs"),
		kong.UsageOnError(),
	)

	client := api.NewClient()
	err := ctx.Run(client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
