package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/anthropics/docs7/api"
	"golang.org/x/term"
)

type AuthCmd struct {
	Ctx7       AuthCtx7Cmd       `cmd:"ctx7" name:"ctx7" help:"Log in to Context7 (opens browser)"`
	Cloudflare AuthCloudflareCmd `cmd:"cloudflare" help:"Configure Cloudflare Browser Rendering credentials"`
	Status     AuthStatusCmd     `cmd:"status" help:"Show authentication status for all providers"`
	Logout     AuthLogoutCmd     `cmd:"logout" help:"Clear stored credentials"`
}

// --- ctx7 ---

type AuthCtx7Cmd struct {
	NoBrowser bool `help:"Print URL instead of opening browser" default:"false"`
}

func (c *AuthCtx7Cmd) Run(client *api.Client) error {
	if err := api.Login(client.BaseURL, c.NoBrowser); err != nil {
		return err
	}
	fmt.Println("Logged in to Context7 successfully.")
	return nil
}

// --- cloudflare ---

type AuthCloudflareCmd struct{}

func (c *AuthCloudflareCmd) Run(_ *api.Client) error {
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

	fmt.Print("Validating token... ")
	if err := api.ValidateCFToken(accountID, token); err != nil {
		fmt.Println("failed")
		return fmt.Errorf("invalid credentials: %w", err)
	}
	fmt.Println("ok")

	if err := api.SaveCFCredentials(&api.CFCredentials{
		AccountID: accountID,
		APIToken:  token,
	}); err != nil {
		return err
	}
	fmt.Println("Cloudflare credentials saved.")
	return nil
}

// --- status ---

type AuthStatusCmd struct{}

func (c *AuthStatusCmd) Run(client *api.Client) error {
	// ctx7
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

	// cloudflare
	creds, err := api.LoadCFCredentials()
	if err == nil {
		fmt.Printf("Cloudflare: configured (account: %s...)\n", creds.AccountID[:8])
	} else {
		fmt.Println("Cloudflare: not configured")
	}

	return nil
}

// --- logout ---

type AuthLogoutCmd struct {
	Provider string `arg:"" optional:"" help:"Provider to log out (ctx7, cloudflare, or all)" default:"all"`
}

func (c *AuthLogoutCmd) Run(_ *api.Client) error {
	switch c.Provider {
	case "ctx7":
		if err := api.ClearTokens(); err != nil {
			fmt.Println("Context7: not logged in.")
		} else {
			fmt.Println("Context7: logged out.")
		}
	case "cloudflare":
		if err := api.ClearCFCredentials(); err != nil {
			fmt.Println("Cloudflare: not configured.")
		} else {
			fmt.Println("Cloudflare: credentials removed.")
		}
	case "all":
		if err := api.ClearTokens(); err == nil {
			fmt.Println("Context7: logged out.")
		}
		if err := api.ClearCFCredentials(); err == nil {
			fmt.Println("Cloudflare: credentials removed.")
		}
	default:
		return fmt.Errorf("unknown provider: %s (use ctx7, cloudflare, or all)", c.Provider)
	}
	return nil
}
