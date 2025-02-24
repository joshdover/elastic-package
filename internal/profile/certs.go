// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package profile

import (
	"bytes"
	"crypto/x509"
	"fmt"
	"io"
	"path/filepath"

	"github.com/elastic/elastic-package/internal/certs"
)

// tlsServices is the list of server TLS certificates that will be
// created in the given path.
var tlsServices = []string{
	"elasticsearch",
	"kibana",
	"package-registry",
	"fleet-server",
}

var (
	// CertificatesDirectory is the path to the certificates directory inside a profile.
	CertificatesDirectory = "certs"

	// CACertificateFile is the path to the CA certificate file inside a profile.
	CACertificateFile = configFile(filepath.Join(CertificatesDirectory, "ca-cert.pem"))

	// CAKeyFile is the path to the CA key file inside a profile.
	CAKeyFile = configFile(filepath.Join(CertificatesDirectory, "ca-key.pem"))
)

// initTLSCertificates initializes all the certificates needed to run the services
// managed by elastic-package stack. It includes a CA, and a pair of keys and
// certificates for each service.
func initTLSCertificates(profilePath string, configMap map[configFile]*simpleFile) error {
	certsDir := filepath.Join(profilePath, CertificatesDirectory)
	caCertFile := filepath.Join(profilePath, string(CACertificateFile))
	caKeyFile := filepath.Join(profilePath, string(CAKeyFile))

	ca, err := initCA(caCertFile, caKeyFile)
	if err != nil {
		return err
	}
	err = certWriterToSimpleFile(configMap, profilePath, caCertFile, ca.WriteCert)
	if err != nil {
		return err
	}
	err = certWriterToSimpleFile(configMap, profilePath, caKeyFile, ca.WriteKey)
	if err != nil {
		return err
	}

	for _, service := range tlsServices {
		certsDir := filepath.Join(certsDir, service)
		caFile := filepath.Join(certsDir, "ca-cert.pem")
		certFile := filepath.Join(certsDir, "cert.pem")
		keyFile := filepath.Join(certsDir, "key.pem")
		cert, err := initServiceTLSCertificates(ca, caCertFile, certFile, keyFile, service)
		if err != nil {
			return err
		}

		err = certWriterToSimpleFile(configMap, profilePath, certFile, cert.WriteCert)
		if err != nil {
			return err
		}
		err = certWriterToSimpleFile(configMap, profilePath, keyFile, cert.WriteKey)
		if err != nil {
			return err
		}

		// Write the CA also in the service directory, so only a directory needs to be mounted
		// for services that need to configure the CA to validate other services certificates.
		err = certWriterToSimpleFile(configMap, profilePath, caFile, ca.WriteCert)
		if err != nil {
			return err
		}
	}

	return nil
}

func certWriterToSimpleFile(configMap map[configFile]*simpleFile, profilePath string, absPath string, writeFile func(w io.Writer) error) error {
	path, err := filepath.Rel(profilePath, absPath)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	err = writeFile(&buf)
	if err != nil {
		return err
	}

	configMap[configFile(path)] = &simpleFile{
		name: path,
		path: absPath,
		body: buf.String(),
	}
	return nil
}

func initCA(certFile, keyFile string) (*certs.Issuer, error) {
	if err := verifyTLSCertificates(certFile, certFile, keyFile, ""); err == nil {
		// Valid CA is already present, load it to check service certificates.
		ca, err := certs.LoadCA(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("error loading CA: %w", err)
		}
		return ca, nil
	}
	ca, err := certs.NewCA()
	if err != nil {
		return nil, fmt.Errorf("error initializing self-signed CA")
	}
	return ca, nil
}

func initServiceTLSCertificates(ca *certs.Issuer, caCertFile string, certFile, keyFile, service string) (*certs.Certificate, error) {
	if err := verifyTLSCertificates(caCertFile, certFile, keyFile, service); err == nil {
		// Certificate already present and valid, load it.
		return certs.LoadCertificate(certFile, keyFile)
	}

	cert, err := ca.Issue(certs.WithName(service))
	if err != nil {
		return nil, fmt.Errorf("error initializing certificate for %q", service)
	}

	return cert, nil
}

func verifyTLSCertificates(caFile, certFile, keyFile, name string) error {
	cert, err := certs.LoadCertificate(certFile, keyFile)
	if err != nil {
		return err
	}

	certPool, err := certs.PoolWithCACertificate(caFile)
	if err != nil {
		return err
	}
	options := x509.VerifyOptions{
		Roots: certPool,
	}
	if name != "" {
		options.DNSName = name
	}
	err = cert.Verify(options)
	if err != nil {
		return err
	}

	return nil
}
