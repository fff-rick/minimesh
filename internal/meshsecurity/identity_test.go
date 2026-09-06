package meshsecurity

import (
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net/url"
	"strings"
	"testing"
)

func TestSPIFFEID(t *testing.T) {
	got := SPIFFEID("minimesh.local", "demo", "inventory")
	if got != "spiffe://minimesh.local/ns/demo/sa/inventory" {
		t.Fatalf("SPIFFEID() = %q", got)
	}
}

func TestRequireIdentity(t *testing.T) {
	identity, _ := url.Parse("spiffe://minimesh.local/ns/demo/sa/order")
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), URIs: []*url.URL{identity}}
	state := tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{certificate}}}
	if err := requireIdentity(map[string]struct{}{identity.String(): {}})(state); err != nil {
		t.Fatalf("allowed identity rejected: %v", err)
	}
	err := requireIdentity(map[string]struct{}{"spiffe://minimesh.local/ns/demo/sa/inventory": {}})(state)
	if err == nil || !strings.Contains(err.Error(), "RBAC denied") {
		t.Fatalf("unauthorized identity error = %v", err)
	}
}

func TestCertificateIdentityRejectsMultipleURISANs(t *testing.T) {
	one, _ := url.Parse("spiffe://minimesh.local/one")
	two, _ := url.Parse("spiffe://minimesh.local/two")
	if _, err := certificateIdentity(&x509.Certificate{URIs: []*url.URL{one, two}}); err == nil {
		t.Fatal("certificateIdentity() accepted multiple URI SANs")
	}
}
