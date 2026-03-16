package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/anthropics/docs7/api"
)

type LoginCmd struct {
	NoBrowser bool `help:"Print URL instead of opening browser" default:"false"`
}

func (c *LoginCmd) Run(client *api.Client) error {
	fmt.Fprintln(os.Stderr, "Note: 'docs7 login' is deprecated. Use 'docs7 auth ctx7' instead.")
	if err := api.Login(client.BaseURL, c.NoBrowser); err != nil {
		return err
	}
	fmt.Println("Logged in successfully.")
	return nil
}

type LogoutCmd struct{}

func (c *LogoutCmd) Run(client *api.Client) error {
	fmt.Fprintln(os.Stderr, "Note: 'docs7 logout' is deprecated. Use 'docs7 auth logout' instead.")
	if err := api.ClearTokens(); err != nil {
		fmt.Println("Not logged in.")
		return nil
	}
	fmt.Println("Logged out.")
	return nil
}

type WhoamiCmd struct{}

func (c *WhoamiCmd) Run(client *api.Client) error {
	fmt.Fprintln(os.Stderr, "Note: 'docs7 whoami' is deprecated. Use 'docs7 auth status' instead.")
	token, _ := api.GetValidToken(client.BaseURL)
	if token == "" {
		fmt.Println("Not logged in.")
		fmt.Println("Run 'docs7 login' to authenticate.")
		return nil
	}
	tokens, err := api.LoadTokens()
	if err != nil {
		return err
	}
	fmt.Println("Logged in")
	fmt.Printf("Token: %s...%s\n", tokens.AccessToken[:8], tokens.AccessToken[len(tokens.AccessToken)-4:])
	if tokens.ExpiresAt > 0 {
		nowMs := time.Now().UnixMilli()
		remainingSec := (tokens.ExpiresAt - nowMs) / 1000
		if remainingSec > 3600 {
			fmt.Printf("Expires in: %.0fh\n", float64(remainingSec)/3600)
		} else if remainingSec > 0 {
			fmt.Printf("Expires in: %dm\n", remainingSec/60)
		}
	}
	return nil
}
