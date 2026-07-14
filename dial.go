package nioclient

// gRPC dial helpers with HTTP/2 keepalive so idle connections survive L4
// idle-eviction (IPVS, cloud LBs, NAT) — see nio issue #239 / check_client.
// Callers supply target and credentials explicitly; this package does not
// read environment variables for dial or TLS.

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// clientKeepalive matches nio check_client: 30s interval, 10s timeout, pings
// while idle (PermitWithoutStream) so connections stay warm with no RPCs.
var clientKeepalive = keepalive.ClientParameters{
	Time:                30 * time.Second,
	Timeout:             10 * time.Second,
	PermitWithoutStream: true,
}

// dial opens a gRPC client connection with the package keepalive defaults.
func dial(target string, creds credentials.TransportCredentials) (*grpc.ClientConn, error) {
	return grpc.NewClient(target,
		grpc.WithTransportCredentials(creds),
		grpc.WithKeepaliveParams(clientKeepalive),
	)
}

// DialCheck dials the check gRPC service with HTTP/2 keepalive (#239).
// Pass nil creds for an insecure channel (local dev only).
func DialCheck(target string, creds credentials.TransportCredentials) (*grpc.ClientConn, error) {
	if target == "" {
		return nil, errors.New("check target is empty")
	}
	if creds == nil {
		creds = insecure.NewCredentials()
	}
	return dial(target, creds)
}

// DialCheckInsecure dials check without TLS (local dev only).
func DialCheckInsecure(target string) (*grpc.ClientConn, error) {
	return DialCheck(target, insecure.NewCredentials())
}

// DialSession dials am.SessionService (nio-client) with the same keepalive as
// DialCheck. Pass nil creds for an insecure channel (local dev only).
func DialSession(target string, creds credentials.TransportCredentials) (*grpc.ClientConn, error) {
	if target == "" {
		return nil, errors.New("session target is empty")
	}
	if creds == nil {
		creds = insecure.NewCredentials()
	}
	return dial(target, creds)
}

// DialSessionInsecure dials session without TLS (local dev only).
func DialSessionInsecure(target string) (*grpc.ClientConn, error) {
	return DialSession(target, insecure.NewCredentials())
}

// LoadTLSCredentials builds mTLS credentials from filesystem paths.
// With both certPath and keyPath empty, returns insecure credentials
// (local dev only). caPath and serverName are optional when using a keypair.
func LoadTLSCredentials(certPath, keyPath, caPath, serverName string) (credentials.TransportCredentials, error) {
	if certPath == "" && keyPath == "" {
		return insecure.NewCredentials(), nil
	}
	if certPath == "" || keyPath == "" {
		return nil, errors.New("cert and key paths must both be set or both empty")
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load client keypair: %w", err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if caPath != "" {
		caPem, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read CA %q: %w", caPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPem) {
			return nil, fmt.Errorf("CA %q contained no certificates", caPath)
		}
		cfg.RootCAs = pool
	}
	if serverName != "" {
		cfg.ServerName = serverName
	}
	return credentials.NewTLS(cfg), nil
}
