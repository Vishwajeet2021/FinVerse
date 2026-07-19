package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// LoadOrGenerateRSAKey loads the RS256 signing key Auth Service uses to mint
// access tokens (docs/11-security-standards.md §3: "the private key lives
// only in Auth Service"). If no key exists at path, one is generated and
// persisted — a documented local-development convenience; in staging/prod
// the key is provisioned via Secrets Manager ahead of time and this path
// always exists, so generation never happens outside dev
// (docs/14-aws-infrastructure.md §8).
func LoadOrGenerateRSAKey(path string) (*rsa.PrivateKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		return parsePEM(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading key file: %w", err)
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("generating RSA key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("creating key directory: %w", err)
	}

	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		return nil, fmt.Errorf("writing key file: %w", err)
	}

	return key, nil
}

func parsePEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found in key file")
	}
	return x509.ParsePKCS1PrivateKey(block.Bytes)
}
