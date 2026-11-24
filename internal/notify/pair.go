package notify

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// A pairing code carries what a new agent needs: the address of the hub, the key of the hub's certificate (the panel's own certificate
// is often self-signed, so the agent trusts that key and not a certificate authority), and a secret that works once.
const codePrefix = "mpn1."

type pairCode struct {
	URL    string `json:"u"`
	Pin    string `json:"p"`
	Secret string `json:"s"`
}

// Token makes a random secret of 32 bytes.
func Token() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("no random numbers: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Hash is how a secret is kept: never the secret itself.
func Hash(secret string) string {
	h := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(h[:])
}

// SameHash compares a secret with a kept hash in constant time.
func SameHash(secret, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(Hash(secret)), []byte(hash)) == 1
}

// ValidHubURL accepts https://host[:port] with nothing after it.
func ValidHubURL(s string) (string, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(s), "/"))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil || u.Fragment != "" {
		return "", errors.New("the address of the hub is written https://name-or-ip:port")
	}
	if len(u.Host) > 260 {
		return "", errors.New("the address of the hub is too long")
	}
	return u.String(), nil
}

// MakeCode writes a pairing code.
func MakeCode(hubURL, pin, secret string) string {
	b, _ := json.Marshal(pairCode{hubURL, pin, secret})
	return codePrefix + base64.RawURLEncoding.EncodeToString(b)
}

// ReadCode reads a pairing code.
func ReadCode(code string) (hubURL, pin, secret string, err error) {
	code = strings.Join(strings.Fields(code), "") // a code copied from a chat may be broken over lines
	bad := errors.New("this is not a pairing code of MCPN")
	if !strings.HasPrefix(code, codePrefix) || len(code) > 2000 {
		return "", "", "", bad
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(code, codePrefix))
	if err != nil {
		return "", "", "", bad
	}
	var c pairCode
	if json.Unmarshal(raw, &c) != nil || c.Secret == "" || len(c.Pin) != 64 {
		return "", "", "", bad
	}
	if c.URL, err = ValidHubURL(c.URL); err != nil {
		return "", "", "", err
	}
	return c.URL, c.Pin, c.Secret, nil
}

// PinOf is the SHA-256 of the public key of the first certificate in a PEM file.
func PinOf(certFile string) (string, error) {
	b, err := os.ReadFile(certFile)
	if err != nil {
		return "", err
	}
	blk, _ := pem.Decode(b)
	if blk == nil {
		return "", errors.New(certFile + " holds no certificate")
	}
	c, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return "", err
	}
	return pinOfCert(c), nil
}

func pinOfCert(c *x509.Certificate) string {
	h := sha256.Sum256(c.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(h[:])
}

// ErrPin is the hub showing a certificate whose key is not the one the agent was paired with.
var ErrPin = errors.New("the hub shows another certificate than when this machine was paired; if the hub's certificate was changed, pair this machine again")

// PinnedClient is an HTTP client that accepts the hub when the key of its certificate is the pinned one, or when its certificate is
// valid for its name by the usual certificate authorities.
func PinnedClient(hubURL, pin string, timeout time.Duration) *http.Client {
	// The name is taken from the address, not from the connection: for an address that is an IP no name is sent, and an empty name
	// would let any valid certificate pass.
	host := ""
	if u, err := url.Parse(hubURL); err == nil {
		host = u.Hostname()
	}
	tr := &http.Transport{
		Proxy:               nil, // the hub is reached directly
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, // checked below
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return ErrPin
				}
				leaf := cs.PeerCertificates[0]
				if subtle.ConstantTimeCompare([]byte(pinOfCert(leaf)), []byte(pin)) == 1 {
					return nil
				}
				inter := x509.NewCertPool()
				for _, c := range cs.PeerCertificates[1:] {
					inter.AddCert(c)
				}
				if host == "" {
					return ErrPin
				}
				if _, err := leaf.Verify(x509.VerifyOptions{DNSName: host, Intermediates: inter}); err == nil {
					return nil
				}
				return ErrPin
			},
		},
	}
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
