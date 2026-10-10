package gitops

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TokenSource supplies the GitHub token for API calls and git over HTTPS.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// StaticToken is a fixed token, such as a personal access token.
type StaticToken string

func (s StaticToken) Token(context.Context) (string, error) {
	if s == "" {
		return "", errors.New("gitops: GitHub token is empty")
	}
	return string(s), nil
}

const (
	jwtLifetime  = 9 * time.Minute // GitHub allows at most 10 minutes
	clockSkew    = time.Minute     // iat is backdated in case the cluster clock runs ahead
	refreshEarly = 5 * time.Minute // renew installation tokens this long before they expire
)

// AppTokenSource issues GitHub App installation tokens. Each token lasts an hour and is
// limited to the repositories passed to NewAppTokenSource; it is cached and renewed shortly
// before it expires.
type AppTokenSource struct {
	appID, installationID int64
	repos                 []string
	key                   *rsa.PrivateKey
	apiURL                string
	http                  *http.Client
	now                   func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewAppTokenSource parses the App's PEM private key (PKCS#1, as GitHub issues it, or PKCS#8).
// repos limits each installation token to those repository names.
func NewAppTokenSource(appID, installationID int64, privateKeyPEM []byte, repos []string, apiURL string, timeout time.Duration) (*AppTokenSource, error) {
	if appID <= 0 || installationID <= 0 {
		return nil, fmt.Errorf("gitops: app_id and installation_id must be positive, got %d and %d", appID, installationID)
	}
	key, err := parseRSAKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("gitops: GitHub App private key: %w", err)
	}
	return &AppTokenSource{
		appID:          appID,
		installationID: installationID,
		repos:          repos,
		key:            key,
		apiURL:         strings.TrimSuffix(apiURL, "/"),
		http:           &http.Client{Timeout: timeout},
		now:            time.Now,
	}, nil
}

func parseRSAKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKCS#1 or PKCS#8 RSA key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("key is %T, want RSA", parsed)
	}
	return key, nil
}

// Token returns a cached installation token, requesting a new one when it is close to expiry.
func (a *AppTokenSource) Token(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.token != "" && a.now().Before(a.expires.Add(-refreshEarly)) {
		return a.token, nil
	}
	token, expires, err := a.fetch(ctx)
	if err != nil {
		return "", err
	}
	a.token, a.expires = token, expires
	return token, nil
}

func (a *AppTokenSource) fetch(ctx context.Context) (string, time.Time, error) {
	jwt, err := a.signJWT()
	if err != nil {
		return "", time.Time{}, err
	}
	in, err := json.Marshal(map[string][]string{"repositories": a.repos})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("encode token request: %w", err)
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", a.installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.apiURL+path, bytes.NewReader(in))
	if err != nil {
		return "", time.Time{}, fmt.Errorf("build POST %s: %w", path, err)
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.http.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("github POST %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		msg, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if err != nil {
			return "", time.Time{}, fmt.Errorf("github POST %s: %d (body unreadable: %w)", path, resp.StatusCode, err)
		}
		return "", time.Time{}, &githubError{Method: http.MethodPost, Path: path, Status: resp.StatusCode,
			Body: strings.ReplaceAll(strings.TrimSpace(string(msg)), jwt, "[REDACTED]")}
	}
	var out struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", time.Time{}, fmt.Errorf("github POST %s: decode: %w", path, err)
	}
	if out.Token == "" || out.ExpiresAt.IsZero() {
		return "", time.Time{}, fmt.Errorf("github POST %s: response has no token or expiry", path)
	}
	return out.Token, out.ExpiresAt, nil
}

// signJWT builds the RS256 JWT that authenticates as the App itself.
func (a *AppTokenSource) signJWT() (string, error) {
	now := a.now()
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{
		"iat": now.Add(-clockSkew).Unix(),
		"exp": now.Add(jwtLifetime).Unix(),
		"iss": fmt.Sprint(a.appID),
	})
	if err != nil {
		return "", fmt.Errorf("encode JWT claims: %w", err)
	}
	signingInput := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}
	return signingInput + "." + enc.EncodeToString(sig), nil
}
