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
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
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

func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "ctx", "ctx7.json")
}

func LoadTokens() (*TokenData, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		return nil, err
	}
	var t TokenData
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func SaveTokens(t *TokenData) error {
	if t.ExpiresAt == 0 && t.ExpiresIn > 0 {
		t.ExpiresAt = time.Now().UnixMilli() + t.ExpiresIn*1000
	}

	dir := filepath.Dir(credentialsPath())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(credentialsPath(), data, 0o600)
}

func ClearTokens() error {
	return os.Remove(credentialsPath())
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
		return "", nil
	}

	newTokens, err := refreshToken(baseURL, tokens.RefreshToken)
	if err != nil {
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

	var wg sync.WaitGroup
	var callbackCode string
	var callbackErr error

	wg.Add(1)
	srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", callbackPort)}
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		defer wg.Done()

		if errParam := r.URL.Query().Get("error"); errParam != "" {
			desc := r.URL.Query().Get("error_description")
			if desc == "" {
				desc = errParam
			}
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, htmlPage("Login Failed", desc, "#dc2626"))
			callbackErr = fmt.Errorf("%s", desc)
			go srv.Close()
			return
		}

		code := r.URL.Query().Get("code")
		rState := r.URL.Query().Get("state")
		if code == "" || rState == "" {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, htmlPage("Login Failed", "Missing code or state", "#dc2626"))
			callbackErr = fmt.Errorf("missing code or state")
			go srv.Close()
			return
		}
		if rState != state {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, htmlPage("Login Failed", "State mismatch", "#dc2626"))
			callbackErr = fmt.Errorf("state mismatch")
			go srv.Close()
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, htmlPage("Login Successful!", "You can close this window and return to the terminal.", "#16a34a"))
		callbackCode = code
		go srv.Close()
	})

	go func() {
		// 5 minute timeout
		time.Sleep(5 * time.Minute)
		srv.Close()
	}()

	go srv.ListenAndServe()

	if noBrowser {
		fmt.Printf("Open this URL in your browser:\n%s\n", authURL)
	} else {
		fmt.Println("Opening browser for login...")
		openBrowser(authURL)
	}

	fmt.Println("Waiting for authorization...")
	wg.Wait()

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
