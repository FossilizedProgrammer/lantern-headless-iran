package main

import (
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/getlantern/fronted"
	"github.com/stretchr/testify/require"
)

func mustParseRanges(t *testing.T, spec string) []*net.IPNet {
	t.Helper()
	nets, err := parseRanges(spec, "")
	require.NoError(t, err)
	return nets
}

func TestParseRanges(t *testing.T) {
	nets := mustParseRanges(t, "23.32.0.0/11, 1.2.3.4")
	require.Len(t, nets, 2)
	require.Equal(t, "23.32.0.0/11", nets[0].String())
	require.Equal(t, "1.2.3.4/32", nets[1].String())
}

func TestParseRangesFileWithComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ranges.txt")
	require.NoError(t, os.WriteFile(path, []byte("# provider ranges\n\n10.0.0.0/8\n10.0.0.0/8\n"), 0644))
	nets, err := parseRanges("", path)
	require.NoError(t, err)
	require.Len(t, nets, 1)
	require.Equal(t, "10.0.0.0/8", nets[0].String())
}

func TestParseRangesSkipsIPv6(t *testing.T) {
	nets, err := parseRanges("2001:db8::/32, 10.0.0.0/8", "")
	require.NoError(t, err)
	require.Len(t, nets, 1)
	require.Equal(t, "10.0.0.0/8", nets[0].String())
}

func TestParseRangesInvalid(t *testing.T) {
	_, err := parseRanges("banana", "")
	require.Error(t, err)
	_, err = parseRanges("", "/nonexistent/file")
	require.Error(t, err)
}

func TestExpandRangesDedupes(t *testing.T) {
	ips, err := expandRanges(mustParseRanges(t, "10.0.0.0/30,10.0.0.0/30"), 0)
	require.NoError(t, err)
	require.Len(t, ips, 4)
}

func TestExpandRangesKeepsAllWithoutCap(t *testing.T) {
	ips, err := expandRanges(mustParseRanges(t, "10.1.0.0/24"), 0)
	require.NoError(t, err)
	require.Len(t, ips, 256)
}

func TestExpandRangesSamplesToMaxIPs(t *testing.T) {
	ips, err := expandRanges(mustParseRanges(t, "10.1.0.0/16"), 100)
	require.NoError(t, err)
	require.Len(t, ips, 100)
	for _, ip := range ips {
		require.NotNil(t, ip.To4())
		require.True(t, ip.To4()[0] == 10 && ip.To4()[1] == 1)
	}
}

func TestExpandRangesRejectsHugeRanges(t *testing.T) {
	_, err := expandRanges(mustParseRanges(t, "0.0.0.0/2"), 0)
	require.Error(t, err)
}

func TestExpandRangesNoRanges(t *testing.T) {
	_, err := expandRanges(nil, 0)
	require.Error(t, err)
}

func TestIncIP(t *testing.T) {
	ip := net.IPv4(10, 0, 0, 255).To4()
	require.True(t, incIP(ip))
	require.Equal(t, "10.0.1.0", ip.String())

	ip = net.IPv4(255, 255, 255, 255).To4()
	require.False(t, incIP(ip))
}

func TestResolveScanSNIsFromProviderMasquerades(t *testing.T) {
	provider := &fronted.Provider{
		Masquerades: []*fronted.Masquerade{
			{Domain: "a248.e.akamai.net"},
			{Domain: "a248.e.akamai.net"},
			{Domain: "*.wild.example"},
			{Domain: ""},
		},
	}
	snis, err := resolveScanSNIs(&scanOptions{}, defaultScanProvider, provider)
	require.NoError(t, err)
	// The alternate Akamai shared hostnames are added so a blocked default
	// SNI does not starve the whole scan.
	require.Equal(t, []string{"a248.e.akamai.net", "a.akamaized.net", "a.akamaihd.net"}, snis)
}

func TestResolveScanSNIsExplicit(t *testing.T) {
	provider := &fronted.Provider{
		Masquerades: []*fronted.Masquerade{{Domain: "from-config.example"}},
	}
	snis, err := resolveScanSNIs(&scanOptions{SNIs: "one.example, two.example"}, defaultScanProvider, provider)
	require.NoError(t, err)
	// Explicit SNIs win; no extras are appended.
	require.Equal(t, []string{"one.example", "two.example"}, snis)
}

func TestResolveScanSNIsNone(t *testing.T) {
	_, err := resolveScanSNIs(&scanOptions{}, "cloudfront", &fronted.Provider{})
	require.Error(t, err)
}

func TestApplyTop(t *testing.T) {
	results := make([]scanResult, 5)
	require.Len(t, applyTop(results, 0), 5, "0 keeps all")
	require.Len(t, applyTop(results, -3), 5, "negative keeps all")
	require.Len(t, applyTop(results, 3), 3)
	require.Len(t, applyTop(results, 10), 5, "top larger than slice keeps all")
}

func TestResolveScanRangesDefaultsForAkamai(t *testing.T) {
	nets, err := resolveScanRanges(&scanOptions{Provider: defaultScanProvider})
	require.NoError(t, err)
	require.NotEmpty(t, nets)
}

func TestResolveScanRangesRequireExplicitForOtherProviders(t *testing.T) {
	_, err := resolveScanRanges(&scanOptions{Provider: "cloudfront"})
	require.Error(t, err)

	nets, err := resolveScanRanges(&scanOptions{Provider: "cloudfront", Ranges: "10.0.0.0/8"})
	require.NoError(t, err)
	require.Len(t, nets, 1)
}

func TestScanRootCAsFromCustomConfig(t *testing.T) {
	// Build a minimal fronting config whose trustedcas holds the test CA, and
	// verify a chain anchored at it through the pool scanRootCAs produced.
	caCert, caKey := mustGenCA(t)
	leafDER := mustGenLeaf(t, caCert, caKey, "a248.e.akamai.net")

	cfg := fmt.Sprintf("trustedcas:\n    - commonname: test root\n      cert: |\n%s", indentPEM(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caCert.Raw})))
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, err := w.Write([]byte(cfg))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	pool, err := scanRootCAs(gz.Bytes())
	require.NoError(t, err)
	require.NoError(t, verifyScanCert([][]byte{leafDER, caCert.Raw}, "a248.e.akamai.net", pool))
}

func TestVerifyScanCert(t *testing.T) {
	caCert, caKey := mustGenCA(t)
	leafDER := mustGenLeaf(t, caCert, caKey, "a248.e.akamai.net")
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	raw := [][]byte{leafDER, caCert.Raw}

	require.NoError(t, verifyScanCert(raw, "a248.e.akamai.net", roots))
	require.Error(t, verifyScanCert(raw, "other.example", roots), "wrong host must fail")
	require.Error(t, verifyScanCert(nil, "a248.e.akamai.net", roots), "no certs must fail")
}

// mustGenCA creates a self-signed CA for tests.
func mustGenCA(t *testing.T) (*x509.Certificate, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return cert, key
}

// mustGenLeaf issues a certificate for host signed by the CA.
func mustGenLeaf(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, host string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		DNSNames:     []string{host},
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.IPv4(127, 0, 0, 1)},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	require.NoError(t, err)
	return der
}

// indentPEM indents a PEM block so it fits into a YAML literal block scalar.
func indentPEM(pemBytes []byte) string {
	var sb strings.Builder
	for _, line := range strings.Split(strings.TrimRight(string(pemBytes), "\n"), "\n") {
		sb.WriteString("        ")
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}
