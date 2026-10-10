//go:build integration

package dbx

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcmysql "github.com/testcontainers/testcontainers-go/modules/mysql"
)

// authority is a throwaway certificate authority, standing in for what a managed database (Aiven...)
// signs its server certificate with.
type authority struct {
	cert    *x509.Certificate
	key     *ecdsa.PrivateKey
	certPEM []byte
}

func newAuthority(t *testing.T, name string) *authority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return &authority{cert: cert, key: key, certPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})}
}

// issueServer signs a server certificate valid for localhost / 127.0.0.1 and returns cert and key PEM.
func (a *authority) issueServer(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano() + 1),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func TestOpenWithCAVerifiesTheServerCertificate(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	ca := newAuthority(t, "BrightBuy test CA")
	serverCert, serverKey := ca.issueServer(t)

	container, err := tcmysql.Run(ctx, "mysql:8.0",
		tcmysql.WithDatabase("tlstest"),
		tcmysql.WithUsername("tls_user"),
		tcmysql.WithPassword("tls-password"),
		testcontainers.WithFiles(
			testcontainers.ContainerFile{Reader: bytes.NewReader(ca.certPEM), ContainerFilePath: "/certs/ca.pem", FileMode: 0o644},
			testcontainers.ContainerFile{Reader: bytes.NewReader(serverCert), ContainerFilePath: "/certs/server-cert.pem", FileMode: 0o644},
			testcontainers.ContainerFile{Reader: bytes.NewReader(serverKey), ContainerFilePath: "/certs/server-key.pem", FileMode: 0o644},
		),
		// require_secure_transport makes the server refuse any unencrypted connection, like Aiven.
		testcontainers.WithCmd(
			"--ssl-ca=/certs/ca.pem", "--ssl-cert=/certs/server-cert.pem", "--ssl-key=/certs/server-key.pem",
			"--require_secure_transport=ON",
		),
	)
	if err != nil {
		t.Fatalf("start mysql: %v", err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "3306/tcp")
	if err != nil {
		t.Fatal(err)
	}
	dsn := fmt.Sprintf("tls_user:tls-password@tcp(%s:%s)/tlstest?parseTime=true", host, port.Port())

	t.Run("the right CA connects over a verified TLS session", func(t *testing.T) {
		db, err := OpenWithCA(dsn, string(ca.certPEM))
		if err != nil {
			t.Fatalf("connection with the right CA failed: %v", err)
		}
		defer db.Close()
		var cipher, value string
		if err := db.QueryRow(`SHOW SESSION STATUS LIKE 'Ssl_cipher'`).Scan(&cipher, &value); err != nil {
			t.Fatal(err)
		}
		if value == "" {
			t.Error("session is not encrypted (Ssl_cipher is empty)")
		}
	})

	t.Run("a different CA is rejected, so an impostor server would be too", func(t *testing.T) {
		other := newAuthority(t, "Some other CA")
		if db, err := OpenWithCA(dsn, string(other.certPEM)); err == nil {
			db.Close()
			t.Fatal("connected although the server's certificate was NOT signed by the supplied CA")
		}
	})

	t.Run("an unencrypted connection is refused by the server", func(t *testing.T) {
		if db, err := Open(dsn + "&tls=false"); err == nil {
			db.Close()
			t.Fatal("plain-text connection was accepted despite require_secure_transport")
		}
	})
}
