package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/ethan-huo/ctx/api"
	"github.com/ethan-huo/ctx/cfrender"
	"golang.org/x/term"
)

type AuthCmd struct {
	Login  AuthLoginCmd  `cmd:"login" help:"Authenticate with a provider"`
	Logout AuthLogoutCmd `cmd:"logout" help:"Clear all stored credentials"`
	Status AuthStatusCmd `cmd:"status" help:"Show authentication status"`
}

// --- login ---

type AuthLoginCmd struct {
	Ctx7       AuthLoginCtx7Cmd       `cmd:"ctx7" name:"ctx7" help:"Log in to Context7 (opens browser)"`
	Cloudflare AuthLoginCloudflareCmd `cmd:"cloudflare" help:"Configure Cloudflare Browser Rendering credentials"`
}

type AuthLoginCtx7Cmd struct {
	NoBrowser bool `help:"Print URL instead of opening browser" default:"false"`
}

func (c *AuthLoginCtx7Cmd) Run(client *api.Client) error {
	if err := api.Login(client.BaseURL, c.NoBrowser); err != nil {
		return err
	}
	fmt.Println("Logged in to Context7 successfully.")
	return nil
}

type AuthLoginCloudflareCmd struct{}

func (c *AuthLoginCloudflareCmd) Run(_ *api.Client) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Cloudflare Account ID: ")
	accountID, _ := reader.ReadString('\n')
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return fmt.Errorf("account ID is required")
	}

	fmt.Print("API Token: ")
	tokenBytes, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Println()
	if err != nil {
		return fmt.Errorf("failed to read token: %w", err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return fmt.Errorf("API token is required")
	}

	// Save first so cfrender.New() can load them for validation
	if err := api.SaveCFCredentials(&api.CFCredentials{
		AccountID: accountID,
		APIToken:  token,
	}); err != nil {
		return err
	}

	fmt.Print("Validating token... ")
	cfClient, err := cfrender.New()
	if err != nil {
		fmt.Println("failed")
		_ = api.ClearCFCredentials()
		return fmt.Errorf("invalid credentials: %w", err)
	}
	if err := cfClient.Validate(); err != nil {
		fmt.Println("failed")
		_ = api.ClearCFCredentials()
		return fmt.Errorf("invalid credentials: %w", err)
	}
	fmt.Println("ok")
	fmt.Println("Cloudflare credentials saved.")
	return nil
}

// --- logout ---

type AuthLogoutCmd struct{}

func (c *AuthLogoutCmd) Run(_ *api.Client) error {
	cleared := false
	if err := api.ClearTokens(); err == nil {
		fmt.Println("Context7: logged out.")
		cleared = true
	}
	if err := api.ClearCFCredentials(); err == nil {
		fmt.Println("Cloudflare: credentials removed.")
		cleared = true
	}
	if !cleared {
		fmt.Println("No credentials to clear.")
	}
	return nil
}

// --- status ---

type AuthStatusCmd struct{}

func (c *AuthStatusCmd) Run(client *api.Client) error {
	token, _ := api.GetValidToken(client.BaseURL)
	if token != "" {
		tokens, err := api.LoadTokens()
		if err == nil {
			fmt.Printf("Context7:   authenticated (%s...%s", tokens.AccessToken[:8], tokens.AccessToken[len(tokens.AccessToken)-4:])
			if tokens.ExpiresAt > 0 {
				remaining := (tokens.ExpiresAt - time.Now().UnixMilli()) / 1000
				if remaining > 3600 {
					fmt.Printf(", expires in %.0fh", float64(remaining)/3600)
				} else if remaining > 0 {
					fmt.Printf(", expires in %dm", remaining/60)
				}
			}
			fmt.Println(")")
		}
	} else {
		fmt.Println("Context7:   not authenticated")
	}

	creds, err := api.LoadCFCredentials()
	if err == nil {
		fmt.Printf("Cloudflare: configured (account: %s...)\n", creds.AccountID[:8])
	} else {
		fmt.Println("Cloudflare: not configured")
	}

	return nil
}
