package nioclient

// gRPC dial helpers with HTTP/2 keepalive so idle connections survive L4
// idle-eviction (IPVS, cloud LBs, NAT) — see nio issue #239 / check_client.

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

// DialCheckFromEnv dials am.CheckService from the relying-party env:
// NIO_CHECK_URI (required — fail fast) plus GRPC_TLS_CERT_PATH / _KEY_PATH /
// _CA_PATH / _DOMAIN. With no TLS cert/key configured the channel is insecure
// (local dev only). Keepalive matches DialCheck (#239).
func DialCheckFromEnv() (*grpc.ClientConn, error) {
	uri := os.Getenv("NIO_CHECK_URI")
	if uri == "" {
		return nil, errors.New("NIO_CHECK_URI is not set")
	}
	creds, err := transportCredsFromEnv(
		"GRPC_TLS_CERT_PATH",
		"GRPC_TLS_KEY_PATH",
		"GRPC_TLS_CA_PATH",
		"GRPC_TLS_DOMAIN",
	)
	if err != nil {
		return nil, err
	}
	return dial(uri, creds)
}

// DialSessionFromEnv dials am.SessionService from the relying-party env:
// NIO_SESSION_URI (required — fail fast) plus the dedicated session-channel TLS
// vars SESSION_GRPC_TLS_CERT_PATH / _KEY_PATH / _CA_PATH / _DOMAIN. These are a
// distinct set from the check-channel GRPC_TLS_* vars (a different, dedicated
// session CA). With no TLS cert/key configured the channel is insecure (local
// dev only). Keepalive matches DialCheck (#239).
func DialSessionFromEnv() (*grpc.ClientConn, error) {
	uri := os.Getenv("NIO_SESSION_URI")
	if uri == "" {
		return nil, errors.New("NIO_SESSION_URI is not set")
	}
	creds, err := transportCredsFromEnv(
		"SESSION_GRPC_TLS_CERT_PATH",
		"SESSION_GRPC_TLS_KEY_PATH",
		"SESSION_GRPC_TLS_CA_PATH",
		"SESSION_GRPC_TLS_DOMAIN",
	)
	if err != nil {
		return nil, err
	}
	return dial(uri, creds)
}

// transportCredsFromEnv builds mTLS credentials from named env vars.
// With neither cert nor key set the channel is insecure (local dev only).
func transportCredsFromEnv(certEnv, keyEnv, caEnv, domainEnv string) (credentials.TransportCredentials, error) {
	certPath := os.Getenv(certEnv)
	keyPath := os.Getenv(keyEnv)
	if certPath == "" && keyPath == "" {
		return insecure.NewCredentials(), nil
	}
	if certPath == "" || keyPath == "" {
		return nil, fmt.Errorf("%s and %s must both be set or both unset", certEnv, keyEnv)
	}

	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return nil, fmt.Errorf("load client keypair: %w", err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	if caPath := os.Getenv(caEnv); caPath != "" {
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
	if domain := os.Getenv(domainEnv); domain != "" {
		cfg.ServerName = domain
	}
	return credentials.NewTLS(cfg), nil
}
