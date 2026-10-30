package sshproxy

import (
	"bytes"
	"context"
	"crypto/x509"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

func TestRealWstunnel(t *testing.T) {
	target := os.Getenv("IRON_SSH_WSTUNNEL_TEST_URL")
	if target == "" {
		t.Skip("set IRON_SSH_WSTUNNEL_TEST_URL and IRON_SSH_WSTUNNEL_TEST_CA for a local OpenSSH/wstunnel fixture")
	}
	cert, err := os.ReadFile(os.Getenv("IRON_SSH_WSTUNNEL_TEST_CA"))
	require.NoError(t, err)
	roots := x509.NewCertPool()
	require.True(t, roots.AppendCertsFromPEM(cert))
	conn, err := transport(context.Background(), target, []string{}, roots)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(3*time.Second)))
	_, err = conn.Write([]byte("SSH-2.0-iron-test\r\n"))
	require.NoError(t, err)
	banner := make([]byte, 256)
	n, err := conn.Read(banner)
	require.NoError(t, err)
	require.Contains(t, string(banner[:n]), "SSH-2.0-")
	// KEX data proves the real server parsed our unmasked client banner.
	newline := bytes.IndexByte(banner[:n], '\n')
	if newline == n-1 {
		n, err = conn.Read(banner)
		require.NoError(t, err)
		require.Positive(t, n)
	}
}
