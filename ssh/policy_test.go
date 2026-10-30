package sshproxy

import (
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"testing"
)

func TestSessionPolicy(t *testing.T) {
	cases := []struct {
		name    string
		packet  []byte
		allowed bool
	}{
		{"session", append([]byte{90}, ssh.Marshal(struct {
			Type                string
			Sender, Window, Max uint32
		}{"session", 0, 1024, 1024})...), true},
		{"forward", append([]byte{90}, ssh.Marshal(struct {
			Type                string
			Sender, Window, Max uint32
		}{"direct-tcpip", 0, 1024, 1024})...), false},
		{"reverse", []byte{80}, false},
	}
	for _, name := range []string{"exec", "signal", "shell", "pty-req", "env", "auth-agent-req@openssh.com", "x11-req"} {
		packet := append([]byte{98}, ssh.Marshal(struct {
			Channel uint32
			Type    string
			Reply   bool
			Payload []byte `ssh:"rest"`
		}{0, name, true, nil})...)
		cases = append(cases, struct {
			name    string
			packet  []byte
			allowed bool
		}{name, packet, name == "exec" || name == "signal"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := downstreamPolicy(tc.packet)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
