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

func TestDialSessionEmptyTarget(t *testing.T) {
	_, err := DialSession("", insecure.NewCredentials())
	if err == nil {
		t.Fatal("expected error for empty target")
	}
}

func TestLoadTLSCredentialsInsecureWhenEmpty(t *testing.T) {
	creds, err := LoadTLSCredentials("", "", "", "")
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

func TestLoadTLSCredentialsPartialPair(t *testing.T) {
	_, err := LoadTLSCredentials("/tmp/only-cert.pem", "", "", "")
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

func TestDialSessionInsecureConstructs(t *testing.T) {
	conn, err := DialSessionInsecure("localhost:1")
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultResolverConfig(t *testing.T) {
	cfg := DefaultResolverConfig()
	if cfg.Capacity != 10000 || cfg.L1TTL != 30*time.Second || cfg.NegTTL != 2*time.Second || cfg.StaleIfError != 0 {
		t.Fatalf("DefaultResolverConfig = %+v", cfg)
	}
}
