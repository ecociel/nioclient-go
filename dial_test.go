package nioclient

import (
	"testing"
	"time"

	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

func TestClientKeepaliveMatchesNio(t *testing.T) {
	// check_client: http2_keep_alive_interval=30s, timeout=10s, while_idle=true
	want := keepalive.ClientParameters{
		Time:                30 * time.Second,
		Timeout:             10 * time.Second,
		PermitWithoutStream: true,
	}
	if clientKeepalive != want {
		t.Fatalf("clientKeepalive = %+v, want %+v", clientKeepalive, want)
	}
}

func TestDialCheckEmptyTarget(t *testing.T) {
	_, err := DialCheck("", insecure.NewCredentials())
	if err == nil {
		t.Fatal("expected error for empty target")
	}
}

func TestDialCheckFromEnvRequiresURI(t *testing.T) {
	t.Setenv("NIO_CHECK_URI", "")
	_, err := DialCheckFromEnv()
	if err == nil {
		t.Fatal("expected error when NIO_CHECK_URI unset")
	}
}

func TestDialSessionFromEnvRequiresURI(t *testing.T) {
	t.Setenv("NIO_SESSION_URI", "")
	_, err := DialSessionFromEnv()
	if err == nil {
		t.Fatal("expected error when NIO_SESSION_URI unset")
	}
}

func TestTransportCredsFromEnvInsecureWhenUnset(t *testing.T) {
	for _, k := range []string{
		"GRPC_TLS_CERT_PATH", "GRPC_TLS_KEY_PATH",
		"GRPC_TLS_CA_PATH", "GRPC_TLS_DOMAIN",
	} {
		t.Setenv(k, "")
	}
	creds, err := transportCredsFromEnv(
		"GRPC_TLS_CERT_PATH", "GRPC_TLS_KEY_PATH",
		"GRPC_TLS_CA_PATH", "GRPC_TLS_DOMAIN",
	)
	if err != nil {
		t.Fatal(err)
	}
	if creds == nil {
		t.Fatal("expected insecure creds")
	}
	conn, err := DialCheck("localhost:1", creds)
	if err != nil {
		t.Fatalf("DialCheck: %v", err)
	}
	_ = conn.Close()
}

func TestTransportCredsFromEnvPartialPair(t *testing.T) {
	t.Setenv("GRPC_TLS_CERT_PATH", "/tmp/only-cert.pem")
	t.Setenv("GRPC_TLS_KEY_PATH", "")
	_, err := transportCredsFromEnv(
		"GRPC_TLS_CERT_PATH", "GRPC_TLS_KEY_PATH",
		"GRPC_TLS_CA_PATH", "GRPC_TLS_DOMAIN",
	)
	if err == nil {
		t.Fatal("expected error when only cert is set")
	}
}

func TestDialCheckInsecureConstructs(t *testing.T) {
	conn, err := DialCheckInsecure("localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}
