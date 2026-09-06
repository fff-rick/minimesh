package meshsecurity

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

type AcceptInfo struct {
	PeerIdentity string
	PeerSerial   string
}

type RelayConfig struct {
	TLS          *tls.Config
	Backend      string
	DialTimeout  time.Duration
	OnConnection func(AcceptInfo)
	OnError      func(error)
}

// ServeRelay terminates mTLS and relays decrypted TCP bytes to the colocated
// plaintext business server. Authorization happens during the TLS handshake.
func ServeRelay(ctx context.Context, listener net.Listener, cfg RelayConfig) error {
	if cfg.TLS == nil || cfg.Backend == "" {
		return fmt.Errorf("meshsecurity: TLS config and backend are required")
	}
	timeout := cfg.DialTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go relayConnection(ctx, connection, cfg, timeout)
	}
}

func relayConnection(ctx context.Context, raw net.Conn, cfg RelayConfig, timeout time.Duration) {
	defer raw.Close()
	secure := tls.Server(raw, cfg.TLS)
	if err := secure.HandshakeContext(ctx); err != nil {
		reportRelayError(cfg, fmt.Errorf("mTLS handshake rejected: %w", err))
		return
	}
	state := secure.ConnectionState()
	identity, err := peerIdentity(state)
	if err != nil {
		reportRelayError(cfg, err)
		return
	}
	serial := ""
	if len(state.PeerCertificates) > 0 {
		serial = state.PeerCertificates[0].SerialNumber.String()
	}
	if cfg.OnConnection != nil {
		cfg.OnConnection(AcceptInfo{PeerIdentity: identity, PeerSerial: serial})
	}

	backend, err := net.DialTimeout("tcp", cfg.Backend, timeout)
	if err != nil {
		reportRelayError(cfg, fmt.Errorf("dial local backend %s: %w", cfg.Backend, err))
		return
	}
	defer backend.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go relayCopy(&wg, backend, secure, cfg)
	go relayCopy(&wg, secure, backend, cfg)
	wg.Wait()
}

func relayCopy(wg *sync.WaitGroup, destination, source net.Conn, cfg RelayConfig) {
	defer wg.Done()
	if _, err := io.Copy(destination, source); err != nil && !errors.Is(err, net.ErrClosed) {
		reportRelayError(cfg, err)
	}
	if closeWriter, ok := destination.(interface{ CloseWrite() error }); ok {
		_ = closeWriter.CloseWrite()
	}
}

func reportRelayError(cfg RelayConfig, err error) {
	if cfg.OnError != nil {
		cfg.OnError(err)
	}
}
