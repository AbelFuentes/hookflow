package alexa

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"
)

const (
	certHost       = "s3.amazonaws.com"
	certPathPrefix = "/echo.api/"
	certSAN        = "echo-api.amazon.com"
	maxCertBytes   = 64 << 10
)

var ErrInvalidSignature = errors.New("alexa: invalid request signature")

type cachedCert struct {
	leaf          *x509.Certificate
	intermediates *x509.CertPool
}

type Verifier struct {
	roots *x509.CertPool // nil = raíces del sistema
	now   func() time.Time
	fetch func(ctx context.Context, url string) ([]byte, error)

	client *http.Client
	mu     sync.Mutex
	cache  map[string]*cachedCert
}

func NewVerifier() *Verifier {
	v := &Verifier{
		now: time.Now,
		client: &http.Client{
			Timeout: 5 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		cache: make(map[string]*cachedCert),
	}
	v.fetch = v.httpFetch
	return v
}

// Verify valida cert URL, cadena, SAN y firma del body crudo.
func (v *Verifier) Verify(ctx context.Context, certURL, signature string, body []byte) error {
	if err := validateCertURL(certURL); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("%w: signature is not base64", ErrInvalidSignature)
	}

	cc, cached := v.lookup(certURL)
	if !cached {
		if cc, err = v.load(ctx, certURL); err != nil {
			return err
		}
	}
	if err := v.checkChain(cc); err != nil {
		v.evict(certURL)
		return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	if !cached {
		v.store(certURL, cc) // solo se cachea lo que ya validó
	}

	pub, ok := cc.leaf.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: certificate key is not RSA", ErrInvalidSignature)
	}
	sum := sha256.Sum256(body)
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return fmt.Errorf("%w: signature mismatch", ErrInvalidSignature)
	}
	return nil
}

func validateCertURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return fmt.Errorf("malformed cert url")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("cert url must use https")
	}
	if !strings.EqualFold(u.Hostname(), certHost) {
		return fmt.Errorf("cert url host must be %s", certHost)
	}
	if p := u.Port(); p != "" && p != "443" {
		return fmt.Errorf("cert url port must be 443")
	}
	if !strings.HasPrefix(path.Clean(u.Path), certPathPrefix) {
		return fmt.Errorf("cert url path must start with %s", certPathPrefix)
	}
	return nil
}

func (v *Verifier) checkChain(cc *cachedCert) error {
	_, err := cc.leaf.Verify(x509.VerifyOptions{
		Roots:         v.roots,
		Intermediates: cc.intermediates,
		CurrentTime:   v.now(),
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	})
	if err != nil {
		return err
	}
	return cc.leaf.VerifyHostname(certSAN)
}

func (v *Verifier) load(ctx context.Context, u string) (*cachedCert, error) {
	data, err := v.fetch(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("fetch cert: %w", err)
	}
	cc, err := parseChain(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidSignature, err)
	}
	return cc, nil
}

func parseChain(data []byte) (*cachedCert, error) {
	var certs []*x509.Certificate
	for {
		var block *pem.Block
		block, data = pem.Decode(data)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, err
		}
		certs = append(certs, c)
	}
	if len(certs) == 0 {
		return nil, errors.New("no certificates in PEM")
	}
	inter := x509.NewCertPool()
	for _, c := range certs[1:] {
		inter.AddCert(c)
	}
	return &cachedCert{leaf: certs[0], intermediates: inter}, nil
}

func (v *Verifier) httpFetch(ctx context.Context, u string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := v.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCertBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCertBytes {
		return nil, errors.New("certificate response too large")
	}
	return data, nil
}

func (v *Verifier) lookup(u string) (*cachedCert, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	cc, ok := v.cache[u]
	return cc, ok
}

func (v *Verifier) store(u string, cc *cachedCert) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cache[u] = cc
}

func (v *Verifier) evict(u string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.cache, u)
}
