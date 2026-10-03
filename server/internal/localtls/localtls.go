// Package localtls is the certificate of the optional local address, such as
// https://192.168.1.20:8443. It is self-signed: the app trusts it because it pinned its SHA-256,
// which it learned over the public address.
package localtls

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io/fs"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The files in the data folder.
const (
	CertFile = "local-cert.pem"
	KeyFile  = "local-key.pem"
)

// validity is long: a phone that pinned the certificate would otherwise lose the local
// address when it expires. Replacing it is `share cert regenerate`.
const validity = 20 * 365 * 24 * time.Hour

// Ensure creates the certificate for host (an IP address or a name) unless there is one.
func Ensure(dataDir, host string) error {
	if _, err := os.Stat(filepath.Join(dataDir, CertFile)); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return Regenerate(dataDir, host)
}

// Regenerate replaces the certificate with a new one for host. Phones notice that the local
// address answers with another certificate, fall back to the public address and learn the
// new fingerprint there.
func Regenerate(dataDir, host string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return err
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Share, local address"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	// The key first: a certificate without its key would stop the listener.
	if err := writeFile(filepath.Join(dataDir, KeyFile), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return err
	}
	return writeFile(filepath.Join(dataDir, CertFile), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644)
}

func writeFile(path string, data []byte, perm os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(perm)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// Fingerprint is the lowercase hex SHA-256 of a certificate, the form phones pin.
func Fingerprint(cert *x509.Certificate) string {
	sum := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(sum[:])
}

// Loader serves the certificate to the local listener and picks up a regenerated one without
// a restart.
type Loader struct {
	dir string

	mu          sync.Mutex
	cert        *tls.Certificate
	fingerprint string
	modTime     time.Time
	checked     time.Time
}

// NewLoader loads the certificate in dataDir.
func NewLoader(dataDir string) (*Loader, error) {
	cert, fp, mod, err := load(dataDir)
	if err != nil {
		return nil, err
	}
	return &Loader{dir: dataDir, cert: cert, fingerprint: fp, modTime: mod, checked: time.Now()}, nil
}

// GetCertificate is for tls.Config.
func (l *Loader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refresh()
	return l.cert, nil
}

// Fingerprint returns the fingerprint of the certificate being served.
func (l *Loader) Fingerprint() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refresh()
	return l.fingerprint
}

// refresh reloads the files if they changed, looking at most every few seconds. l.mu is held.
func (l *Loader) refresh() {
	if time.Since(l.checked) < 5*time.Second {
		return
	}
	l.checked = time.Now()
	st, err := os.Stat(filepath.Join(l.dir, CertFile))
	if err != nil || st.ModTime().Equal(l.modTime) {
		return
	}
	cert, fp, mod, err := load(l.dir)
	if err != nil {
		log.Printf("localtls: keeping the old certificate: %v", err)
		return
	}
	l.cert, l.fingerprint, l.modTime = cert, fp, mod
}

func load(dir string) (*tls.Certificate, string, time.Time, error) {
	certPath := filepath.Join(dir, CertFile)
	st, err := os.Stat(certPath)
	if err != nil {
		return nil, "", time.Time{}, err
	}
	pair, err := tls.LoadX509KeyPair(certPath, filepath.Join(dir, KeyFile))
	if err != nil {
		return nil, "", time.Time{}, err
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, "", time.Time{}, err
	}
	pair.Leaf = leaf
	return &pair, Fingerprint(leaf), st.ModTime(), nil
}
