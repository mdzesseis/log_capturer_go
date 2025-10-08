package sinks

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// SecretManager interface for secret management
type SecretManager interface {
	GetSecret(key string) (string, error)
}

// basicSecretManager is a simple implementation for basic use
type basicSecretManager struct{}

// GetSecret retrieves secret from environment variables
func (sm *basicSecretManager) GetSecret(key string) (string, error) {
	value := os.Getenv(key)
	if value == "" {
		return "", fmt.Errorf("secret %s not found", key)
	}
	return value, nil
}

// NewBasicSecretManager creates a basic secret manager
func NewBasicSecretManager() SecretManager {
	return &basicSecretManager{}
}

// TLSConfig configuration for TLS connections
type TLSConfig struct {
	Enabled            bool   `yaml:"enabled"`
	CertFile           string `yaml:"cert_file"`
	KeyFile            string `yaml:"key_file"`
	CAFile             string `yaml:"ca_file"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

// createTLSConfig creates a TLS configuration from config
func createTLSConfig(config TLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.InsecureSkipVerify,
	}

	if config.CertFile != "" && config.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load cert/key pair: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	if config.CAFile != "" {
		caCert, err := os.ReadFile(config.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caCertPool
	}

	return tlsConfig, nil
}