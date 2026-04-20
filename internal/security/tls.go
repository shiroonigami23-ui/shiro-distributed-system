package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

type TLSOptions struct {
	CAFile             string
	CertFile           string
	KeyFile            string
	ServerName         string
	InsecureSkipVerify bool
}

func BuildTLSConfig(opts TLSOptions) (*tls.Config, error) {
	if opts.CAFile == "" && opts.CertFile == "" && opts.KeyFile == "" && opts.ServerName == "" && !opts.InsecureSkipVerify {
		return nil, nil
	}

	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         opts.ServerName,
		InsecureSkipVerify: opts.InsecureSkipVerify,
	}

	if opts.CAFile != "" {
		ca, err := os.ReadFile(opts.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read ca file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(ca) {
			return nil, fmt.Errorf("append ca certs failed")
		}
		cfg.RootCAs = pool
	}

	if opts.CertFile != "" || opts.KeyFile != "" {
		if opts.CertFile == "" || opts.KeyFile == "" {
			return nil, fmt.Errorf("both cert and key files are required for mTLS client certs")
		}
		cert, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client cert/key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	return cfg, nil
}
