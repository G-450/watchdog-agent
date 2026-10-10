package gitops

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeAppAPI serves POST /app/installations/{id}/access_tokens, checking the JWT like GitHub does.
type fakeAppAPI struct {
	t      *testing.T
	pub    *rsa.PublicKey
	now    func() time.Time
	status int // 0 = 201 Created

	mu    sync.Mutex
	calls int
	repos []string
}

func (f *fakeAppAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if r.Method != http.MethodPost || r.URL.Path != "/app/installations/42/access_tokens" {
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	jwt := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		f.t.Fatalf("malformed JWT %q", jwt)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		f.t.Fatalf("decode signature: %v", err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(f.pub, crypto.SHA256, sum[:], sig); err != nil {
		f.t.Errorf("JWT signature invalid: %v", err)
	}
	var claims struct {
		Iat, Exp int64
		Iss      string
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if err := json.Unmarshal(raw, &claims); err != nil {
		f.t.Fatalf("decode claims: %v", err)
	}
	now := f.now().Unix()
	if claims.Iss != "7" || claims.Iat > now || claims.Exp <= now || claims.Exp-claims.Iat > 600 {
		f.t.Errorf("bad claims %+v at %d", claims, now)
	}
	var body struct{ Repositories []string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		f.t.Errorf("decode body: %v", err)
	}
	f.repos = body.Repositories

	if f.status != 0 {
		w.WriteHeader(f.status)
		_, _ = w.Write([]byte(`{"message":"Integration not found","jwt":"` + jwt + `"}`))
		return
	}
	writeJSON(f.t, w, http.StatusCreated, map[string]any{
		"token":      "ghs_installation" + string(rune('0'+f.calls)),
		"expires_at": f.now().Add(time.Hour).UTC().Format(time.RFC3339),
	})
}

func newAppSource(t *testing.T, status int) (*AppTokenSource, *fakeAppAPI, *time.Time) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	api := &fakeAppAPI{t: t, pub: &key.PublicKey, now: clock, status: status}
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)

	src, err := NewAppTokenSource(7, 42, pemKey, []string{"watchdog-infra"}, srv.URL+"/", 5*time.Second)
	if err != nil {
		t.Fatalf("NewAppTokenSource: %v", err)
	}
	src.now = clock
	return src, api, &now
}

func TestAppTokenSource(t *testing.T) {
	src, api, now := newAppSource(t, 0)
	ctx := context.Background()

	first, err := src.Token(ctx)
	if err != nil || first != "ghs_installation1" {
		t.Fatalf("Token = %q, %v", first, err)
	}
	if len(api.repos) != 1 || api.repos[0] != "watchdog-infra" {
		t.Errorf("token not limited to the infra repo: %v", api.repos)
	}

	*now = now.Add(50 * time.Minute)
	if again, _ := src.Token(ctx); again != first || api.calls != 1 {
		t.Errorf("want the cached token at 50m, got %q after %d calls", again, api.calls)
	}

	*now = now.Add(6 * time.Minute) // 56m: inside the refresh margin
	if renewed, err := src.Token(ctx); err != nil || renewed != "ghs_installation2" || api.calls != 2 {
		t.Errorf("want a renewed token, got %q, %v after %d calls", renewed, err, api.calls)
	}
}

func TestAppTokenSourceErrorRedactsJWT(t *testing.T) {
	src, _, _ := newAppSource(t, http.StatusNotFound)
	_, err := src.Token(context.Background())
	if err == nil || !strings.Contains(err.Error(), "Integration not found") || !strings.Contains(err.Error(), "404") {
		t.Fatalf("want GitHub's message and status, got %v", err)
	}
	if strings.Contains(err.Error(), "eyJ") {
		t.Errorf("error leaks the JWT: %v", err)
	}
}

func TestNewAppTokenSource(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	tests := []struct {
		name          string
		appID, instID int64
		key           []byte
		wantErr       bool
	}{
		{name: "PKCS#1", appID: 1, instID: 2, key: pkcs1},
		{name: "PKCS#8", appID: 1, instID: 2, key: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})},
		{name: "not PEM", appID: 1, instID: 2, key: []byte("hello"), wantErr: true},
		{name: "missing app id", instID: 2, key: pkcs1, wantErr: true},
		{name: "missing installation id", appID: 1, key: pkcs1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewAppTokenSource(tt.appID, tt.instID, tt.key, nil, "https://api.github.com", time.Second)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// failingTokens makes the generator's token lookup fail.
type failingTokens struct{}

func (failingTokens) Token(context.Context) (string, error) {
	return "", &githubError{Method: "POST", Path: "/app/installations/42/access_tokens", Status: 401, Body: "bad JWT"}
}

func TestApplyTokenFailure(t *testing.T) {
	requireGit(t)
	e := newTestEnv(t)
	e.gen.tokens = failingTokens{}
	err := e.gen.Apply(context.Background(), loadFixtureRecs(t)[:1])
	if err == nil || !strings.Contains(err.Error(), "get GitHub token") || !strings.Contains(err.Error(), "bad JWT") {
		t.Fatalf("want the token error, got %v", err)
	}
	if n := e.api.count(""); n != 0 {
		t.Errorf("no GitHub calls without a token, got %v", e.api.calls)
	}
}
