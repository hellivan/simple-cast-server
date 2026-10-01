package chromecast

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"testing"
	"time"
)

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestConnReconnectsAfterServerDropsConnection(t *testing.T) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{selfSignedCert(t)}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	accepted := make(chan net.Conn, 4)
	go func() {
		for {
			cn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				if err := cn.(*tls.Conn).Handshake(); err == nil {
					accepted <- cn
				}
			}()
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	c := newConn("127.0.0.1", port, nil)
	defer c.close()

	if err := c.connect(); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if !c.isConnected() {
		t.Fatal("expected connected")
	}

	// Server drops the connection; the client must notice.
	first := <-accepted
	first.Close()
	eventually(t, func() bool { return !c.isConnected() })

	// The next connect must redial successfully.
	if err := c.connect(); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if !c.isConnected() {
		t.Fatal("expected connected after reconnect")
	}
	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Fatal("server did not see a second connection")
	}
}
