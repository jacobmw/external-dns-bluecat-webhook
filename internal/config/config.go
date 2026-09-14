package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// Config is runtime configuration for the webhook sidecar.
type Config struct {
	ListenAddress  string
	HealthAddress  string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	LogLevel       string
	DryRun         bool
	DomainFilter   []string
	ExcludeDomains []string
	ZoneIDFilter   []string

	ConfigFile        string
	Host              string
	Username          string
	Password          string
	DNSConfiguration  string
	View              string
	RootZone          string
	DNSServerName     string
	DNSDeployType     string
	SkipTLSVerify     bool
	CAFile            string
	HTTPClientTimeout time.Duration
}

type fileConfig struct {
	BluecatHost      string `json:"bluecatHost"`
	BluecatUsername  string `json:"bluecatUsername"`
	BluecatPassword  string `json:"bluecatPassword"`
	DNSConfiguration string `json:"dnsConfiguration"`
	DNSServerName    string `json:"dnsServerName"`
	DNSDeployType    string `json:"dnsDeployType"`
	View             string `json:"dnsView"`
	RootZone         string `json:"rootZone"`
	SkipTLSVerify    bool   `json:"skipTLSVerify"`
	CAFile           string `json:"caFile"`
}

// Load parses flags and environment, then optionally overlays a JSON config file.
func Load(args []string) (*Config, error) {
	cfg := &Config{
		ListenAddress:     "127.0.0.1:8888",
		HealthAddress:     ":8080",
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		LogLevel:          "info",
		DNSDeployType:     "no-deploy",
		HTTPClientTimeout: 30 * time.Second,
	}

	fs := pflag.NewFlagSet("external-dns-bluecat-webhook", pflag.ContinueOnError)
	fs.StringVar(&cfg.ListenAddress, "listen-address", cfg.ListenAddress, "address for the ExternalDNS webhook API (localhost only is recommended)")
	fs.StringVar(&cfg.HealthAddress, "health-address", cfg.HealthAddress, "address for /healthz")
	fs.DurationVar(&cfg.ReadTimeout, "read-timeout", cfg.ReadTimeout, "webhook HTTP read timeout")
	fs.DurationVar(&cfg.WriteTimeout, "write-timeout", cfg.WriteTimeout, "webhook HTTP write timeout")
	fs.StringVar(&cfg.LogLevel, "log-level", envOr("LOG_LEVEL", cfg.LogLevel), "log level")
	fs.BoolVar(&cfg.DryRun, "dry-run", false, "log changes without calling BlueCat")
	fs.StringSliceVar(&cfg.DomainFilter, "domain-filter", nil, "limit managed zones to these domains (repeatable)")
	fs.StringSliceVar(&cfg.ExcludeDomains, "exclude-domains", nil, "domains to exclude (repeatable)")
	fs.StringSliceVar(&cfg.ZoneIDFilter, "zone-id-filter", nil, "limit managed zones to these BlueCat zone IDs")
	fs.StringVar(&cfg.ConfigFile, "bluecat-config-file", os.Getenv("BLUECAT_CONFIG_FILE"), "optional JSON config file (same keys as the in-tree BlueCat provider)")
	fs.StringVar(&cfg.Host, "bluecat-host", os.Getenv("BLUECAT_HOST"), "BlueCat Address Manager base URL, e.g. https://bam.example.com")
	fs.StringVar(&cfg.Username, "bluecat-username", os.Getenv("BLUECAT_USERNAME"), "API username")
	fs.StringVar(&cfg.Password, "bluecat-password", os.Getenv("BLUECAT_PASSWORD"), "API password")
	fs.StringVar(&cfg.DNSConfiguration, "bluecat-dns-configuration", os.Getenv("BLUECAT_DNS_CONFIGURATION"), "optional BAM configuration name (informational)")
	fs.StringVar(&cfg.View, "bluecat-dns-view", os.Getenv("BLUECAT_DNS_VIEW"), "optional DNS view name used to filter zones")
	fs.StringVar(&cfg.RootZone, "bluecat-root-zone", os.Getenv("BLUECAT_ROOT_ZONE"), "root zone used to discover zones (contains filter)")
	fs.StringVar(&cfg.DNSServerName, "bluecat-dns-server-name", os.Getenv("BLUECAT_DNS_SERVER_NAME"), "when set, enables zone deploy after changes")
	fs.StringVar(&cfg.DNSDeployType, "bluecat-dns-deploy-type", envOr("BLUECAT_DNS_DEPLOY_TYPE", cfg.DNSDeployType), "no-deploy, quick-deploy, or dynamic")
	fs.BoolVar(&cfg.SkipTLSVerify, "bluecat-skip-tls-verify", envBool("BLUECAT_SKIP_TLS_VERIFY"), "skip TLS verification for BAM (do not use with --bluecat-ca-file)")
	fs.StringVar(&cfg.CAFile, "bluecat-ca-file", os.Getenv("BLUECAT_CA_FILE"), "PEM file of extra CA certificates to trust for BAM TLS")
	fs.DurationVar(&cfg.HTTPClientTimeout, "bluecat-http-timeout", cfg.HTTPClientTimeout, "timeout for BAM HTTP calls")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if cfg.ConfigFile != "" {
		if err := overlayFile(cfg); err != nil {
			return nil, err
		}
	}
	if v, ok := os.LookupEnv("BLUECAT_USERNAME"); ok && v != "" {
		cfg.Username = v
	}
	if v, ok := os.LookupEnv("BLUECAT_PASSWORD"); ok && v != "" {
		cfg.Password = v
	}
	if v, ok := os.LookupEnv("BLUECAT_CA_FILE"); ok && v != "" {
		cfg.CAFile = v
	}

	switch cfg.DNSDeployType {
	case "no-deploy", "quick-deploy", "dynamic":
	default:
		return nil, fmt.Errorf("invalid dns deploy type %q (want no-deploy, quick-deploy, or dynamic)", cfg.DNSDeployType)
	}
	if cfg.Host == "" {
		return nil, fmt.Errorf("bluecat host is required (--bluecat-host or BLUECAT_HOST)")
	}
	if cfg.SkipTLSVerify && cfg.CAFile != "" {
		return nil, fmt.Errorf("cannot set both --bluecat-skip-tls-verify and --bluecat-ca-file")
	}
	return cfg, nil
}

func overlayFile(cfg *Config) error {
	raw, err := os.ReadFile(cfg.ConfigFile)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}
	var file fileConfig
	if err := json.Unmarshal(raw, &file); err != nil {
		return fmt.Errorf("parse config file: %w", err)
	}
	if file.BluecatHost != "" {
		cfg.Host = file.BluecatHost
	}
	if file.BluecatUsername != "" {
		cfg.Username = file.BluecatUsername
	}
	if file.BluecatPassword != "" {
		cfg.Password = file.BluecatPassword
	}
	if file.DNSConfiguration != "" {
		cfg.DNSConfiguration = file.DNSConfiguration
	}
	if file.DNSServerName != "" {
		cfg.DNSServerName = file.DNSServerName
	}
	if file.DNSDeployType != "" {
		cfg.DNSDeployType = file.DNSDeployType
	}
	if file.View != "" {
		cfg.View = file.View
	}
	if file.RootZone != "" {
		cfg.RootZone = file.RootZone
	}
	if file.CAFile != "" {
		cfg.CAFile = file.CAFile
	}
	cfg.SkipTLSVerify = file.SkipTLSVerify
	return nil
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

func envBool(key string) bool {
	v := strings.ToLower(os.Getenv(key))
	return v == "1" || v == "true" || v == "yes"
}
