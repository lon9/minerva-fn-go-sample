package callminerva

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultTokenPath is where the platform mounts the projected
	// ServiceAccount token into a Function Pod.
	DefaultTokenPath = "/var/run/secrets/minerva/workload-identity/token"

	exchangeProcedure = "paas.iam.v1.IamWorkloadIdentityService/ExchangeWorkloadIdentityToken"

	// refreshMargin renews the access token this long before it expires so a
	// request never starts with a token about to lapse.
	refreshMargin = time.Minute
	httpTimeout   = 10 * time.Second
	maxBodyBytes  = 1 << 20
)

// WorkloadIdentity authenticates a Function to the Minerva API as the Function's
// own ServiceAccount: it exchanges the projected token for a short-lived
// access token (cached until shortly before expiry) and sends API calls with it.
//
// It speaks the Connect protocol's JSON encoding over plain HTTP, so it needs
// no Minerva SDK. This file is the part meant to move into the SDK.
type WorkloadIdentity struct {
	baseURL   string
	tokenPath string
	http      *http.Client
	now       func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewWorkloadIdentity returns a client that exchanges tokens at baseURL (the
// Minerva gateway origin) using the projected token read from tokenPath.
func NewWorkloadIdentity(baseURL, tokenPath string) *WorkloadIdentity {
	return &WorkloadIdentity{
		baseURL:   strings.TrimRight(baseURL, "/"),
		tokenPath: tokenPath,
		http:      &http.Client{Timeout: httpTimeout},
		now:       time.Now,
	}
}

// Token returns a valid access token, exchanging the projected token when none
// is cached or the cached one is about to expire. The projected token is
// re-read from disk on every exchange because the kubelet rotates it.
func (w *WorkloadIdentity) Token(ctx context.Context) (string, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.token != "" && w.now().Before(w.expires.Add(-refreshMargin)) {
		return w.token, nil
	}
	projected, err := os.ReadFile(w.tokenPath)
	if err != nil {
		return "", fmt.Errorf("read projected token: %w", err)
	}
	var resp struct {
		AccessToken      string      `json:"accessToken"`
		ExpiresInSeconds json.Number `json:"expiresInSeconds"`
	}
	req := map[string]string{"projectedToken": strings.TrimSpace(string(projected))}
	if err := w.post(ctx, w.baseURL, exchangeProcedure, "", req, &resp); err != nil {
		return "", fmt.Errorf("exchange workload identity token: %w", err)
	}
	seconds, err := resp.ExpiresInSeconds.Int64()
	if err != nil || resp.AccessToken == "" {
		return "", fmt.Errorf("exchange workload identity token: malformed response")
	}
	w.token = resp.AccessToken
	w.expires = w.now().Add(time.Duration(seconds) * time.Second)
	return w.token, nil
}

// Call invokes the unary Connect procedure (for example
// "paas.project.v1.ProjectService/GetProject") on baseURL as this Function.
func (w *WorkloadIdentity) Call(ctx context.Context, baseURL, procedure string, req, resp any) error {
	token, err := w.Token(ctx)
	if err != nil {
		return err
	}
	return w.post(ctx, strings.TrimRight(baseURL, "/"), procedure, token, req, resp)
}

func (w *WorkloadIdentity) post(ctx context.Context, baseURL, procedure, bearer string, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/"+procedure, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		httpReq.Header.Set("Authorization", "Bearer "+bearer)
	}
	httpResp, err := w.http.Do(httpReq)
	if err != nil {
		return err
	}
	defer httpResp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(httpResp.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		return connectError(httpResp.StatusCode, data)
	}
	if err := json.Unmarshal(data, resp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// connectError renders a Connect error body ({"code","message"}); a non-Connect
// body (a proxy error page, say) falls back to the HTTP status.
func connectError(status int, data []byte) error {
	var e struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) == nil && e.Code != "" {
		return fmt.Errorf("%s: %s (http %d)", e.Code, e.Message, status)
	}
	return fmt.Errorf("http %d", status)
}
