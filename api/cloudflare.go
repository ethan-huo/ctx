package api

import (
	"github.com/ethan-huo/ctx/config"
)

// CFCredentials bridges the old API to the unified config system.
type CFCredentials struct {
	AccountID string
	APIToken  string
}

func LoadCFCredentials() (*CFCredentials, error) {
	id, token, err := config.LoadCloudflare()
	if err != nil {
		return nil, err
	}
	return &CFCredentials{AccountID: id, APIToken: token}, nil
}

func SaveCFCredentials(c *CFCredentials) error {
	return config.UpdateCredentials(func(creds *config.Credentials) {
		creds.Cloudflare = config.CloudflareCreds{
			AccountID: c.AccountID,
			APIToken:  c.APIToken,
		}
	})
}

func ClearCFCredentials() error {
	return config.UpdateCredentials(func(creds *config.Credentials) {
		creds.Cloudflare = config.CloudflareCreds{}
	})
}
