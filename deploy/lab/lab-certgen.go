package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func main() {
	out := "/out/lab-certs"
	must(os.MkdirAll(out, 0o755))

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	must(err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Control One Lab"},
			CommonName:   "controlone-lab-nodeagent",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(3650 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	must(err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	keyDER, err := x509.MarshalECPrivateKey(key)
	must(err)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	must(os.WriteFile(filepath.Join(out, "client.crt"), certPEM, 0o644))
	must(os.WriteFile(filepath.Join(out, "ca.crt"), certPEM, 0o644))
	must(os.WriteFile(filepath.Join(out, "client.key"), keyPEM, 0o600))
}
