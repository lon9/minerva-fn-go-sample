// Package callminerva is a sample Minerva Function that calls the Minerva API
// as itself, using the Function's Workload Identity (docs/function-design.md
// 6.1). On each request it fetches the Project named by MINERVA_PROJECT_ID and
// returns it as JSON.
package callminerva

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
)

const getProjectProcedure = "paas.project.v1.ProjectService/GetProject"

// Config is the Function's settings, read from environment variables.
type Config struct {
	// APIURL is the Minerva gateway origin (MINERVA_API_URL). The token exchange
	// and the API calls both go through it.
	APIURL string
	// ProjectID is the Project to read (MINERVA_PROJECT_ID).
	ProjectID string
	// TokenPath is the projected token file (MINERVA_TOKEN_PATH, optional).
	TokenPath string
}

func configFromEnv() Config {
	tokenPath := os.Getenv("MINERVA_TOKEN_PATH")
	if tokenPath == "" {
		tokenPath = DefaultTokenPath
	}
	return Config{
		APIURL:    os.Getenv("MINERVA_API_URL"),
		ProjectID: os.Getenv("MINERVA_PROJECT_ID"),
		TokenPath: tokenPath,
	}
}

func (c Config) validate() error {
	if c.APIURL == "" {
		return fmt.Errorf("MINERVA_API_URL is not set")
	}
	if c.ProjectID == "" {
		return fmt.Errorf("MINERVA_PROJECT_ID is not set")
	}
	return nil
}

// handler is built once at startup from the environment.
var handler = newHandler(configFromEnv())

// GetProject is the Function's entry point (--entry-point GetProject). It is a
// plain net/http handler, so the source needs no Functions Framework
// dependency and no go.sum.
func GetProject(w http.ResponseWriter, r *http.Request) {
	handler.ServeHTTP(w, r)
}

// newHandler builds the HTTP handler. A bad Config is reported on every request
// rather than at startup so the cause is visible in the response.
func newHandler(cfg Config) http.Handler {
	identity := NewWorkloadIdentity(cfg.APIURL, cfg.TokenPath)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := cfg.validate(); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var resp json.RawMessage
		req := map[string]string{"projectId": cfg.ProjectID}
		if err := identity.Call(r.Context(), cfg.APIURL, getProjectProcedure, req, &resp); err != nil {
			log.Printf("get project %s: %v", cfg.ProjectID, err)
			writeError(w, http.StatusBadGateway, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	})
}

func writeError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
