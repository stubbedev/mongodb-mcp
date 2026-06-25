package source

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/stubbedev/mongodb-mcp/internal/config"
)

// sshDialer dials TCP connections through an SSH server. It satisfies the
// MongoDB driver's options.ContextDialer interface so the driver transparently
// tunnels every connection (handshake, monitoring, queries) over SSH.
type sshDialer struct {
	client *ssh.Client
}

// newSSHDialer establishes the SSH connection described by cfg.
func newSSHDialer(cfg *config.SSHConfig) (*sshDialer, error) {
	auths, err := sshAuthMethods(cfg)
	if err != nil {
		return nil, err
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("ssh: no usable authentication methods")
	}

	hostKeyCallback, err := sshHostKeyCallback(cfg)
	if err != nil {
		return nil, err
	}

	clientCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            auths,
		HostKeyCallback: hostKeyCallback,
		Timeout:         15 * time.Second,
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	client, err := ssh.Dial("tcp", addr, clientCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}
	return &sshDialer{client: client}, nil
}

// DialContext implements options.ContextDialer. ssh.Client.Dial is not
// context-aware, so we run it in a goroutine and honour ctx cancellation.
func (d *sshDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		conn, err := d.client.Dial(network, address)
		ch <- result{conn, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-ch:
		if r.err != nil {
			return nil, fmt.Errorf("ssh tunnel to %s: %w", address, r.err)
		}
		return r.conn, nil
	}
}

// Close tears down the SSH connection.
func (d *sshDialer) Close() error {
	if d.client == nil {
		return nil
	}
	return d.client.Close()
}

func sshAuthMethods(cfg *config.SSHConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	// SSH agent first, mirroring an interactive ssh client.
	if cfg.UseAgent {
		sock := os.Getenv("SSH_AUTH_SOCK")
		if sock == "" {
			return nil, fmt.Errorf("ssh: use_agent set but SSH_AUTH_SOCK is empty")
		}
		conn, err := net.Dial("unix", sock)
		if err != nil {
			return nil, fmt.Errorf("ssh: connect to agent: %w", err)
		}
		ag := agent.NewClient(conn)
		methods = append(methods, ssh.PublicKeysCallback(ag.Signers))
	}

	// Identity file.
	if cfg.IdentityFile != "" {
		key, err := os.ReadFile(config.ExpandPath(cfg.IdentityFile))
		if err != nil {
			return nil, fmt.Errorf("ssh: read identity file: %w", err)
		}
		var signer ssh.Signer
		if cfg.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(cfg.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(key)
		}
		if err != nil {
			return nil, fmt.Errorf("ssh: parse identity file: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	// Password last.
	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}

	return methods, nil
}

func sshHostKeyCallback(cfg *config.SSHConfig) (ssh.HostKeyCallback, error) {
	if cfg.InsecureIgnoreHostKey {
		//nolint:gosec // explicitly opted in via config for trusted networks/testing.
		return ssh.InsecureIgnoreHostKey(), nil
	}
	path := config.ExpandPath(cfg.KnownHostsFile)
	if path == "" {
		path = config.ExpandPath("~/.ssh/known_hosts")
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("ssh: load known_hosts %q (set insecure_ignore_host_key to bypass): %w", path, err)
	}
	return cb, nil
}
