// Command iron-ssh runs the sshpiper companion to iron-proxy.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ironsh/iron-proxy/internal/dnsguard"
	sshproxy "github.com/ironsh/iron-proxy/ssh"
	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

func main() {
	path := flag.String("config", "", "trusted SSH companion YAML configuration")
	flag.Parse()
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(*path, logger); err != nil {
		logger.Error("SSH companion stopped", "error", err.Error())
		os.Exit(1)
	}
}

func run(path string, logger *slog.Logger) error {
	var cfg struct {
		Listen          string    `yaml:"listen"`
		HostKey         yaml.Node `yaml:"host_key"`
		ClientPublicKey string    `yaml:"client_public_key"`
		AuthorizeURL    string    `yaml:"authorize_url"`
		TokenEnv        string    `yaml:"token_env"`
		GrantsFile      string    `yaml:"grants_file"`
		DenyCIDRs       []string  `yaml:"upstream_deny_cidrs"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("SSH configuration unavailable")
	}
	if yaml.Unmarshal(data, &cfg) != nil {
		return fmt.Errorf("SSH configuration invalid")
	}
	if cfg.Listen == "" {
		return fmt.Errorf("SSH listen address required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	host, err := sshproxy.ParseHostKey(ctx, cfg.HostKey, logger)
	if err != nil {
		return err
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(cfg.ClientPublicKey))
	if err != nil {
		return fmt.Errorf("sandbox public key invalid")
	}
	var auth sshproxy.Authorizer
	if cfg.AuthorizeURL != "" && cfg.GrantsFile == "" {
		token := os.Getenv(cfg.TokenEnv)
		if token == "" {
			return fmt.Errorf("SSH proxy token unavailable")
		}
		auth = sshproxy.HTTPAuthorizer(cfg.AuthorizeURL, token)
	} else if cfg.GrantsFile != "" && cfg.AuthorizeURL == "" {
		auth = sshproxy.FileAuthorizer(cfg.GrantsFile)
	} else {
		return fmt.Errorf("exactly one SSH authorizer required")
	}
	if cfg.DenyCIDRs == nil {
		cfg.DenyCIDRs = dnsguard.DefaultDenyCIDRs
	}
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("SSH listener unavailable")
	}
	defer listener.Close()
	server := &sshproxy.Server{HostKey: host, ClientPublicKey: key, Authorize: auth, DenyCIDRs: cfg.DenyCIDRs, Logger: logger}
	return server.Serve(ctx, listener)
}
