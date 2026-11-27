// Package certs makes the TLS client certificates that identify accounts
// towards Murmur, and the server certificate of the built-in server. Murmur ties a registration to the certificate's hash, so
// a certificate lives as long as its account.
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

// Generate makes a self-signed client certificate (ECDSA P-256, 30 years).
func Generate(name string) (certPEM, keyPEM []byte, err error) {
	return generate(name, x509.ExtKeyUsageClientAuth)
}

// GenerateServer makes a self-signed server certificate, as Murmur makes
// one on its first start.
func GenerateServer(name string) (certPEM, keyPEM []byte, err error) {
	return generate(name, x509.ExtKeyUsageServerAuth)
}

func generate(name string, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: name},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(30, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func Load(certPEM, keyPEM []byte) (tls.Certificate, error) {
	return tls.X509KeyPair(certPEM, keyPEM)
}

// LoadOrCreateServer keeps one server certificate in dir (cert.pem and
// key.pem), made on the first start, so clients can pin it.
func LoadOrCreateServer(dir string) (tls.Certificate, error) {
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	certPEM, err := os.ReadFile(certPath) // #nosec G304 -- the operator's CERT_DIR, a fixed name
	if errors.Is(err, fs.ErrNotExist) {
		var keyPEM []byte
		if certPEM, keyPEM, err = GenerateServer("Kallisti"); err != nil {
			return tls.Certificate{}, err
		}
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return tls.Certificate{}, err
		}
		if err = os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
			return tls.Certificate{}, err
		}
		if err = os.WriteFile(certPath, certPEM, 0o644); err != nil { // #nosec G306 -- the public certificate (the key is 0600)
			return tls.Certificate{}, err
		}
	} else if err != nil {
		return tls.Certificate{}, err
	}
	keyPEM, err := os.ReadFile(keyPath) // #nosec G304 -- the operator's CERT_DIR, a fixed name
	if err != nil {
		return tls.Certificate{}, err
	}
	return Load(certPEM, keyPEM)
}
