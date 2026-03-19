package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/ethan-huo/ctx/config"
)

const (
	clientID     = "2veBSofhicRBguUT"
	callbackPort = 52417
	redirectURI  = "http://localhost:52417/callback"
)

type TokenData struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	ExpiresAt    int64  `json:"expires_at,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

func LoadTokens() (*TokenData, error) {
	creds, err := config.LoadCredentials()
	if err != nil {
		return nil, err
	}
	c := creds.Ctx7
	if c.AccessToken == "" {
		return nil, fmt.Errorf("not logged in to Context7")
	}
	return &TokenData{
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		TokenType:    c.TokenType,
		ExpiresIn:    c.ExpiresIn,
		ExpiresAt:    c.ExpiresAt,
		Scope:        c.Scope,
	}, nil
}

func SaveTokens(t *TokenData) error {
	if t.ExpiresAt == 0 && t.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().UnixMilli() + t.ExpiresIn*1000
	}
	return config.UpdateCredentials(func(creds *config.Credentials) {
		creds.Ctx7 = config.Ctx7Creds{
			AccessToken:  t.AccessToken,
			RefreshToken: t.RefreshToken,
			TokenType:    t.TokenType,
			ExpiresIn:    t.ExpiresIn,
			ExpiresAt:    t.ExpiresAt,
			Scope:        t.Scope,
		}
	})
}

func ClearTokens() error {
	return config.UpdateCredentials(func(creds *config.Credentials) {
		creds.Ctx7 = config.Ctx7Creds{}
	})
}

func IsTokenExpired(t *TokenData) bool {
	if t.ExpiresAt == 0 {
		return false
	}
	return time.Now().UnixMilli() > t.ExpiresAt-60000
}

// GetValidToken returns a valid access token, refreshing if needed.
func GetValidToken(baseURL string) (string, error) {
	if key := os.Getenv("CONTEXT7_API_KEY"); key != "" {
		return key, nil
	}

	tokens, err := LoadTokens()
	if err != nil {
		return "", nil
	}

	if !IsTokenExpired(tokens) {
		return tokens.AccessToken, nil
	}

	if tokens.RefreshToken == "" {
		fmt.Fprintf(os.Stderr, "Context7 token expired and no refresh token available. Run: ctx auth login\n")
		return "", nil
	}

	newTokens, err := refreshToken(baseURL, tokens.RefreshToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Context7 token refresh failed: %v — falling back to anonymous. Run: ctx auth login\n", err)
		return "", nil
	}
	if err := SaveTokens(newTokens); err != nil {
		return "", err
	}
	return newTokens.AccessToken, nil
}

func refreshToken(baseURL, refresh string) (*TokenData, error) {
	resp, err := http.PostForm(baseURL+"/api/oauth/token", url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refresh},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh failed: HTTP %d", resp.StatusCode)
	}

	var t TokenData
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Login runs the full OAuth PKCE flow: start server, open browser, wait for callback.
func Login(baseURL string, noBrowser bool) error {
	verifier, challenge := generatePKCE()
	state := generateState()
	authURL := buildAuthURL(baseURL, challenge, state)

	done := make(chan struct{}, 1)
	var callbackCode string
	var callbackErr error

	mux := http.NewServeMux()
	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", callbackPort),
		Handler: mux,
	}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			if desc == "" {
				desc = errParam
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, htmlPage("Login Failed", desc, "#dc2626"))
			callbackErr = fmt.Errorf("%s", desc)
			select {
			case done <- struct{}{}:
			default:
			}
			go srv.Close()
			return
		}

		code := r.URL.Query().Get("code")
		rState := r.URL.Query().Get("state")
		if code == "" || rState == "" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, htmlPage("Login Failed", "Missing code or state", "#dc2626"))
			callbackErr = fmt.Errorf("missing code or state")
			select {
			case done <- struct{}{}:
			default:
			}
			go srv.Close()
			return
		}
		if rState != state {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, htmlPage("Login Failed", "State mismatch", "#dc2626"))
			callbackErr = fmt.Errorf("state mismatch")
			select {
			case done <- struct{}{}:
			default:
			}
			go srv.Close()
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, htmlPage("Login Successful!", "You can close this window and return to the terminal.", "#16a34a"))
		callbackCode = code
		select {
		case done <- struct{}{}:
		default:
		}
		go srv.Close()
	})

	go func() {
		time.Sleep(5 * time.Minute)
		callbackErr = fmt.Errorf("login timed out after 5 minutes — no browser callback received")
		select {
		case done <- struct{}{}:
		default:
		}
		srv.Close()
	}()

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			callbackErr = fmt.Errorf("failed to start callback server on port %d: %w", callbackPort, err)
			select {
			case done <- struct{}{}:
			default:
			}
		}
	}()

	if noBrowser {
		fmt.Printf("Open this URL in your browser:\n%s\n", authURL)
	} else {
		fmt.Println("Opening browser for login...")
		openBrowser(authURL)
	}

	fmt.Println("Waiting for authorization...")
	<-done

	if callbackErr != nil {
		return callbackErr
	}

	// Exchange code for tokens
	tokens, err := exchangeCode(baseURL, callbackCode, verifier)
	if err != nil {
		return fmt.Errorf("token exchange failed: %w", err)
	}

	return SaveTokens(tokens)
}

func exchangeCode(baseURL, code, verifier string) (*TokenData, error) {
	resp, err := http.PostForm(baseURL+"/api/oauth/token", url.Values{
		"grant_type":    {"authorization_code"},
		"client_id":     {clientID},
		"code":          {code},
		"code_verifier": {verifier},
		"redirect_uri":  {redirectURI},
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var t TokenData
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, err
	}
	return &t, nil
}

func generatePKCE() (verifier, challenge string) {
	b := make([]byte, 32)
	rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)
	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func buildAuthURL(baseURL, challenge, state string) string {
	params := url.Values{
		"client_id":             {clientID},
		"redirect_uri":         {redirectURI},
		"code_challenge":       {challenge},
		"code_challenge_method": {"S256"},
		"state":                {state},
		"scope":                {"profile email"},
		"response_type":        {"code"},
	}
	return baseURL + "/api/oauth/authorize?" + params.Encode()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		cmd = exec.Command("open", url)
	}
	cmd.Start()
}

func htmlPage(title, message, color string) string {
	message = strings.ReplaceAll(message, "&", "&amp;")
	message = strings.ReplaceAll(message, "<", "&lt;")
	message = strings.ReplaceAll(message, ">", "&gt;")
	return fmt.Sprintf(`<!DOCTYPE html>
<html><head><title>%s</title></head>
<body style="font-family:system-ui;display:flex;justify-content:center;align-items:center;height:100vh;margin:0;background:#f9fafb">
<div style="text-align:center;padding:2rem">
<h1 style="color:%s">%s</h1>
<p style="color:#6b7280">%s</p>
</div></body></html>`, title, color, title, message)
}
