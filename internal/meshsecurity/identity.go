// Package meshsecurity provides SPIFFE-style X.509 workload identity for
// MiniMesh sidecars. It intentionally does not claim SPIFFE Workload API
// conformance; certificates are mounted Kubernetes Secrets in Stage 15.
package meshsecurity

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/url"
	"os"
	"path"
)

type Files struct {
	CA   string
	Cert string
	Key  string
}

func SPIFFEID(trustDomain, namespace, service string) string {
	return "spiffe://" + trustDomain + path.Join("/ns", namespace, "sa", service)
}

func ClientTLSConfig(files Files, expectedIdentity, serverName string) (*tls.Config, error) {
	roots, err := loadCAPool(files.CA)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		RootCAs:    roots,
		ServerName: serverName,
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			certificate, loadErr := tls.LoadX509KeyPair(files.Cert, files.Key)
			return &certificate, loadErr
		},
		VerifyConnection: requireIdentity(map[string]struct{}{expectedIdentity: {}}),
	}, nil
}

func ServerTLSConfig(files Files, allowedIdentities []string) (*tls.Config, error) {
	roots, err := loadCAPool(files.CA)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(allowedIdentities))
	for _, identity := range allowedIdentities {
		allowed[identity] = struct{}{}
	}
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		ClientAuth: tls.RequireAndVerifyClientCert,
		ClientCAs:  roots,
		NextProtos: []string{"h2"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			certificate, loadErr := tls.LoadX509KeyPair(files.Cert, files.Key)
			return &certificate, loadErr
		},
		VerifyConnection: requireIdentity(allowed),
	}, nil
}

func CurrentIdentity(files Files) (identity, serial string, err error) {
	certificate, err := tls.LoadX509KeyPair(files.Cert, files.Key)
	if err != nil {
		return "", "", err
	}
	if len(certificate.Certificate) == 0 {
		return "", "", fmt.Errorf("certificate chain is empty")
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return "", "", err
	}
	identity, err = certificateIdentity(leaf)
	if err != nil {
		return "", "", err
	}
	return identity, leaf.SerialNumber.String(), nil
}

func requireIdentity(allowed map[string]struct{}) func(tls.ConnectionState) error {
	return func(state tls.ConnectionState) error {
		identity, err := peerIdentity(state)
		if err != nil {
			return err
		}
		if _, ok := allowed[identity]; !ok {
			return fmt.Errorf("mesh RBAC denied peer identity %q", identity)
		}
		return nil
	}
}

func peerIdentity(state tls.ConnectionState) (string, error) {
	if len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) == 0 {
		return "", fmt.Errorf("peer certificate was not verified")
	}
	return certificateIdentity(state.VerifiedChains[0][0])
}

func certificateIdentity(certificate *x509.Certificate) (string, error) {
	if len(certificate.URIs) != 1 {
		return "", fmt.Errorf("peer certificate must contain exactly one URI SAN")
	}
	identity := certificate.URIs[0]
	if identity.Scheme != "spiffe" || identity.Host == "" || identity.RawQuery != "" || identity.Fragment != "" {
		return "", fmt.Errorf("invalid SPIFFE identity %q", identity.String())
	}
	if _, err := url.ParseRequestURI(identity.String()); err != nil {
		return "", fmt.Errorf("invalid SPIFFE identity: %w", err)
	}
	return identity.String(), nil
}

func loadCAPool(filename string) (*x509.CertPool, error) {
	bundle, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(bundle) {
		return nil, fmt.Errorf("CA bundle %q contains no certificates", filename)
	}
	return pool, nil
}
