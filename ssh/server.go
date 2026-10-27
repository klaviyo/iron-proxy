package sshproxy

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"log/slog"
	"net"
	"reflect"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"gopkg.in/yaml.v3"
)

// Server accepts only its paired sandbox's disposable public key.
type Server struct {
	HostKey          ssh.Signer
	ClientPublicKey  ssh.PublicKey
	Authorize        Authorizer
	DenyCIDRs        []string
	TLSRootCAs       *x509.CertPool
	Logger           *slog.Logger
	RenewalInterval  time.Duration
	HandshakeTimeout time.Duration
}

func (s *Server) Serve(parent context.Context, listener net.Listener) error {
	if s.HostKey == nil || s.ClientPublicKey == nil || s.Authorize == nil || s.Logger == nil {
		return errors.New("incomplete SSH configuration")
	}
	ctx, cancel := context.WithCancel(parent)
	var wg sync.WaitGroup
	defer wg.Wait()
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		wg.Add(1)
		go func() { defer wg.Done(); s.handle(ctx, conn) }()
	}
}

func (s *Server) handle(parent context.Context, conn net.Conn) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	started := time.Now()
	action, reason := "deny", "authentication_failed"
	var route, principal, grantID string
	defer func() {
		rejected := ""
		if action == "deny" {
			rejected = "ssh_authorization"
			if reason == "capability_denied" {
				rejected = "ssh_policy"
			}
		}
		s.Logger.Info("ssh_connection", "action", action, "reason", reason, "route", route, "principal", principal, "grant_id", grantID, "duration_ms", time.Since(started).Milliseconds(), "request_transforms", []string{"ssh_authorization"}, "rejected_by", rejected)
	}()
	timeout := s.HandshakeTimeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	if conn.SetDeadline(time.Now().Add(timeout)) != nil {
		return
	}
	var approved *Grant
	var auth Authorization
	var upstream net.Conn
	defer func() {
		if upstream != nil {
			_ = upstream.Close()
		}
	}()
	config := &ssh.PiperConfig{MaxAuthTries: 1}
	config.PublicKeyCallback = func(meta ssh.ConnMetadata, key ssh.PublicKey, _ ssh.ChallengeContext) (*ssh.Upstream, error) {
		if !bytes.Equal(key.Marshal(), s.ClientPublicKey.Marshal()) {
			return nil, errors.New("SSH access denied")
		}
		if len(meta.User()) == 0 || len(meta.User()) > 128 {
			return nil, errors.New("invalid SSH route")
		}
		route = meta.User()
		auth = Authorization{Route: route, PublicKey: string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(key)))}
		grant, err := s.Authorize(ctx, auth)
		if err != nil {
			return nil, errors.New("SSH access denied")
		}
		if err = validateGrant(grant); err != nil {
			return nil, err
		}
		credential, err := signer(ctx, grant.PrivateKey, s.Logger)
		if err != nil {
			return nil, err
		}
		expected, _, _, _, err := ssh.ParseAuthorizedKey([]byte(grant.HostKey))
		if err != nil {
			return nil, errors.New("invalid upstream host identity")
		}
		if upstream != nil {
			_ = upstream.Close()
		}
		upstream, err = transport(ctx, grant.Destination, s.DenyCIDRs, s.TLSRootCAs)
		if err != nil {
			return nil, errors.New("SSH upstream unavailable")
		}
		if err = upstream.SetDeadline(time.Now().Add(timeout)); err != nil {
			return nil, err
		}
		algorithms := []string{expected.Type()}
		if expected.Type() == ssh.KeyAlgoRSA {
			algorithms = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
		}
		approved = grant
		principal = grant.Principal
		grantID = grant.ID
		return &ssh.Upstream{Conn: upstream, Address: grant.Destination, ClientConfig: ssh.ClientConfig{User: grant.Username, HostKeyAlgorithms: algorithms, Auth: []ssh.AuthMethod{ssh.PublicKeys(credential)}, HostKeyCallback: func(_ string, _ net.Addr, actual ssh.PublicKey) error {
			if !bytes.Equal(expected.Marshal(), actual.Marshal()) {
				return errors.New("SSH host identity mismatch")
			}
			return nil
		}}}, nil
	}
	config.AddHostKey(s.HostKey)
	pipe, err := ssh.NewSSHPiperConn(conn, config)
	if err != nil {
		return
	}
	defer pipe.Close()
	if approved == nil {
		return
	}
	if conn.SetDeadline(time.Time{}) != nil || upstream.SetDeadline(time.Time{}) != nil {
		return
	}
	action, reason = "allow", "connection_closed"
	leaseCtx, leaseCancel := context.WithCancel(ctx)
	defer leaseCancel()
	done := make(chan struct{})
	go func() { defer close(done); s.renew(leaseCtx, cancel, auth, approved) }()
	if err := pipe.WaitWithHook(upstreamPolicy, downstreamPolicy); err != nil {
		reason = "connection_terminated"
		if errors.Is(err, errCapabilityDenied) {
			action, reason = "deny", "capability_denied"
		}
	}
	leaseCancel()
	<-done
}

func validateGrant(g *Grant) error {
	if g == nil || g.ID == "" || g.Principal == "" || g.Generation == "" || g.Username == "" || g.HostKey == "" || g.Destination == "" || !g.ExpiresAt.After(time.Now()) || time.Until(g.ExpiresAt) > 15*time.Minute {
		return errors.New("invalid SSH grant")
	}
	return nil
}

func (s *Server) renew(ctx context.Context, cancel context.CancelFunc, a Authorization, g *Grant) {
	interval := s.RenewalInterval
	if interval == 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	expiry := time.AfterFunc(time.Until(g.ExpiresAt), cancel)
	defer expiry.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			next, err := s.Authorize(ctx, a)
			if err != nil || validateGrant(next) != nil || !sameGrant(g, next) {
				cancel()
				return
			}
			expiry.Reset(time.Until(next.ExpiresAt))
		}
	}
}
func sameGrant(a, b *Grant) bool {
	if a == nil || b == nil {
		return false
	}
	x, y := *a, *b
	x.ExpiresAt = time.Time{}
	y.ExpiresAt = time.Time{}
	return reflect.DeepEqual(x, y)
}

// ParseHostKey resolves a proxy-only credential source.
func ParseHostKey(ctx context.Context, source yaml.Node, logger *slog.Logger) (ssh.Signer, error) {
	return signer(ctx, source, logger)
}
