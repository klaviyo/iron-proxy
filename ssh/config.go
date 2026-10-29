// Package sshproxy implements an isolated sshpiper credential boundary.
package sshproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ironsh/iron-proxy/internal/transform/secrets"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

// Grant is delivered only over the authenticated proxy control channel.
type Grant struct {
	ID          string    `json:"id" yaml:"id"`
	Principal   string    `json:"principal" yaml:"principal"`
	Destination string    `json:"destination" yaml:"destination"`
	Generation  string    `json:"generation" yaml:"generation"`
	Username    string    `json:"username" yaml:"username"`
	HostKey     string    `json:"host_key" yaml:"host_key"`
	TLSIdentity bool      `json:"tls_identity,omitempty" yaml:"tls_identity"`
	PrivateKey  yaml.Node `json:"-" yaml:"private_key"`
	ExpiresAt   time.Time `json:"expires_at" yaml:"expires_at"`
}

// Authorization identifies a sandbox key and a configured route, never a user.
type Authorization struct {
	Route       string `json:"route"`
	PublicKey   string `json:"public_key"`
	Destination string `json:"destination,omitempty"`
}

// Authorizer is called again during active connections; errors revoke access.
type Authorizer func(context.Context, Authorization) (*Grant, error)

// HTTPAuthorizer uses a proxy-only bearer token and does not follow redirects.
func HTTPAuthorizer(endpoint, token string) Authorizer {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return func(ctx context.Context, a Authorization) (*Grant, error) {
		body, err := json.Marshal(a)
		if err != nil {
			return nil, errors.New("authorization encoding failed")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, errors.New("authorization URL invalid")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return nil, errors.New("authorization unavailable")
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("SSH access denied")
		}
		// JSON is decoded as YAML so source nodes retain the existing resolver contract.
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return nil, errors.New("authorization response unavailable")
		}
		var g Grant
		if yaml.Unmarshal(data, &g) != nil {
			return nil, errors.New("invalid authorization response")
		}
		return &g, nil
	}
}

func signer(ctx context.Context, node yaml.Node, logger *slog.Logger) (ssh.Signer, error) {
	source, err := secrets.BuildSource(node, logger)
	if err != nil {
		return nil, errors.New("SSH credential source invalid")
	}
	value, err := source.Get(ctx)
	if err != nil {
		return nil, errors.New("SSH credential unavailable")
	}
	key, err := ssh.ParsePrivateKey([]byte(value))
	if err != nil {
		return nil, errors.New("SSH credential invalid")
	}
	return key, nil
}

// FileAuthorizer supports trusted local provisioning and rereads on every check.
func FileAuthorizer(path string) Authorizer {
	return func(ctx context.Context, a Authorization) (*Grant, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.New("SSH grants unavailable")
		}
		var entries []struct {
			Route     string `yaml:"route"`
			PublicKey string `yaml:"public_key"`
			Grant     Grant  `yaml:"grant"`
		}
		if yaml.Unmarshal(data, &entries) != nil {
			return nil, errors.New("SSH grants invalid")
		}
		for _, e := range entries {
			if e.Route == a.Route && e.PublicKey == a.PublicKey {
				if a.Destination != "" && a.Destination != e.Grant.Destination {
					continue
				}
				return &e.Grant, nil
			}
		}
		return nil, fmt.Errorf("SSH access denied")
	}
}
