package alexa

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

const testCertURL = "https://s3.amazonaws.com/echo.api/test.pem"

func mustKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// newTestPKI crea una CA de prueba y un leaf firmado por ella con el SAN dado.
func newTestPKI(t *testing.T, dnsName string) (*x509.CertPool, []byte, *rsa.PrivateKey) {
	t.Helper()
	caKey, leafKey := mustKey(t), mustKey(t)

	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}

	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: dnsName},
		DNSNames:     []string{dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	return roots, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), leafKey
}

func sign(t *testing.T, key *rsa.PrivateKey, body []byte) string {
	t.Helper()
	sum := sha256.Sum256(body)
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func newTestVerifier(t *testing.T, dnsName string, fetches *int) (*Verifier, *rsa.PrivateKey) {
	t.Helper()
	roots, chain, key := newTestPKI(t, dnsName)
	v := NewVerifier()
	v.roots = roots
	v.fetch = func(context.Context, string) ([]byte, error) {
		*fetches++
		return chain, nil
	}
	return v, key
}

func TestVerify(t *testing.T) {
	var fetches int
	v, key := newTestVerifier(t, certSAN, &fetches)
	ctx := context.Background()
	body := []byte(`{"hello":"alexa"}`)
	sig := sign(t, key, body)

	if err := v.Verify(ctx, testCertURL, sig, body); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if err := v.Verify(ctx, testCertURL, sig, []byte(`{"hello":"evil"}`)); err == nil {
		t.Fatal("tampered body accepted")
	}
	if err := v.Verify(ctx, testCertURL, "!!!", body); err == nil {
		t.Fatal("non-base64 signature accepted")
	}
	if err := v.Verify(ctx, "https://evil.com/echo.api/x.pem", sig, body); err == nil {
		t.Fatal("bad cert url accepted")
	}
	if fetches != 1 {
		t.Fatalf("cert fetched %d times, want 1 (cache)", fetches)
	}
}

func TestVerify_WrongSAN(t *testing.T) {
	var fetches int
	v, key := newTestVerifier(t, "evil.example.com", &fetches)
	body := []byte(`{}`)
	if err := v.Verify(context.Background(), testCertURL, sign(t, key, body), body); err == nil {
		t.Fatal("cert without echo-api.amazon.com SAN accepted")
	}
}

func TestVerify_UntrustedChain(t *testing.T) {
	var fetches int
	v, key := newTestVerifier(t, certSAN, &fetches)
	v.roots = x509.NewCertPool() // raíz vacía: nada es confiable
	body := []byte(`{}`)
	if err := v.Verify(context.Background(), testCertURL, sign(t, key, body), body); err == nil {
		t.Fatal("untrusted chain accepted")
	}
}

func TestValidateCertURL(t *testing.T) {
	good := []string{
		"https://s3.amazonaws.com/echo.api/echo-api-cert.pem",
		"https://s3.amazonaws.com:443/echo.api/cert.pem",
		"HTTPS://S3.AMAZONAWS.COM/echo.api/cert.pem",
	}
	bad := []string{
		"http://s3.amazonaws.com/echo.api/c.pem",
		"https://s3.amazonaws.com.evil.com/echo.api/c.pem",
		"https://s3.amazonaws.com:8443/echo.api/c.pem",
		"https://s3.amazonaws.com/EcHo.api/c.pem",
		"https://s3.amazonaws.com/echo.api/../evil/c.pem",
		"https://user@s3.amazonaws.com/echo.api/c.pem",
		"not a url",
	}
	for _, u := range good {
		if err := validateCertURL(u); err != nil {
			t.Errorf("%q rejected: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := validateCertURL(u); err == nil {
			t.Errorf("%q accepted", u)
		}
	}
}
