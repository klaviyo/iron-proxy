package sshproxy

import (
	"errors"
	"golang.org/x/crypto/ssh"
)

var errCapabilityDenied = errors.New("SSH capability denied")

func downstreamPolicy(packet []byte) (ssh.PipePacketHookMethod, []byte, error) {
	deny := func() (ssh.PipePacketHookMethod, []byte, error) {
		return ssh.PipePacketHookTransform, nil, errCapabilityDenied
	}
	if len(packet) == 0 {
		return deny()
	}
	switch packet[0] {
	case 80:
		return deny()
	case 90:
		var open struct {
			Type      string
			Sender    uint32
			Window    uint32
			MaxPacket uint32
			Extra     []byte `ssh:"rest"`
		}
		if ssh.Unmarshal(packet[1:], &open) != nil || open.Type != "session" {
			return deny()
		}
	case 98:
		var req struct {
			Channel   uint32
			Type      string
			WantReply bool
			Payload   []byte `ssh:"rest"`
		}
		if ssh.Unmarshal(packet[1:], &req) != nil {
			return deny()
		}
		switch req.Type {
		case "exec", "signal":
		case "subsystem":
			var sub struct{ Name string }
			if ssh.Unmarshal(req.Payload, &sub) != nil || sub.Name != "sftp" {
				return deny()
			}
		default:
			return deny()
		}
	}
	return ssh.PipePacketHookTransform, packet, nil
}

func upstreamPolicy(packet []byte) (ssh.PipePacketHookMethod, []byte, error) {
	if len(packet) == 0 || packet[0] == 90 {
		return ssh.PipePacketHookTransform, nil, errCapabilityDenied
	}
	if packet[0] == 80 {
		var req struct {
			Type      string
			WantReply bool
			Payload   []byte `ssh:"rest"`
		}
		if ssh.Unmarshal(packet[1:], &req) != nil {
			return ssh.PipePacketHookTransform, nil, errors.New("invalid upstream request")
		}
		if req.WantReply {
			return ssh.PipePacketHookReply, []byte{82}, nil
		}
		return ssh.PipePacketHookTransform, nil, nil
	}
	return ssh.PipePacketHookTransform, packet, nil
}
