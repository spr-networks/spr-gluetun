package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var TEST_PREFIX = os.Getenv("TEST_PREFIX")

// ConfigFile holds the full plugin config, including secrets (0600).
var ConfigFile = TEST_PREFIX + "/configs/spr-gluetun/config.json"

// GluetunEnvFile is consumed by the gluetun compose service via env_file (0600).
var GluetunEnvFile = TEST_PREFIX + "/configs/spr-gluetun/gluetun.env"

// GluetunAuthFile is gluetun's control-server auth config; the path is inside
// gluetun's /gluetun state dir, shared through the plugin state mount (0600).
var GluetunAuthFile = TEST_PREFIX + "/state/plugins/spr-gluetun/gluetun/auth/config.toml"

var Configmtx sync.RWMutex

// Config is the persisted plugin configuration. Secret fields are write-only
// through the HTTP API: they are accepted on PUT and never echoed on GET.
type Config struct {
	Provider string
	VPNType  string // "wireguard" or "openvpn"

	// wireguard
	WireguardPrivateKey   string   `json:",omitempty"`
	WireguardPresharedKey string   `json:",omitempty"`
	WireguardAddresses    []string `json:",omitempty"`
	// wireguard, "custom" provider only
	WireguardPublicKey    string `json:",omitempty"`
	WireguardEndpointIP   string `json:",omitempty"`
	WireguardEndpointPort int    `json:",omitempty"`

	// openvpn
	OpenVPNUser     string `json:",omitempty"`
	OpenVPNPassword string `json:",omitempty"`

	// server filters
	ServerCountries []string `json:",omitempty"`
	ServerCities    []string `json:",omitempty"`

	// dns + firewall
	DNSAddress              string   `json:",omitempty"`
	DisableDNSOverTLS       bool     `json:",omitempty"`
	FirewallOutboundSubnets []string `json:",omitempty"`

	// gluetun control-server API key. Generated on first run, never exposed
	// through the plugin HTTP API.
	ControlAPIKey string `json:",omitempty"`
}

// RedactedConfig is what GET /config returns: no secrets, only "is set" flags.
type RedactedConfig struct {
	Provider string
	VPNType  string

	WireguardKeySet          bool
	WireguardPresharedKeySet bool
	WireguardAddresses       []string
	WireguardPublicKey       string
	WireguardEndpointIP      string
	WireguardEndpointPort    int

	OpenVPNUser        string
	OpenVPNPasswordSet bool

	ServerCountries []string
	ServerCities    []string

	DNSAddress              string
	DisableDNSOverTLS       bool
	FirewallOutboundSubnets []string

	Configured bool
}

var gConfig = Config{}

var (
	// country/city filter entries ("Netherlands", "New York")
	locationRe = regexp.MustCompile(`^[A-Za-z][A-Za-z .'-]{0,63}$`)
	// openvpn credentials: printable ASCII, excluding characters that are
	// meaningful to dotenv parsers or shells: " ' ` \ # $
	credentialRe = regexp.MustCompile(`^[A-Za-z0-9 _.@!%*()+=:,/?~\[\]{}|;<>^&-]{1,128}$`)
)

func validVPNTypes() []string { return []string{"wireguard", "openvpn"} }

func isValidProvider(name string) bool {
	for _, p := range Providers {
		if p.Name == name {
			return true
		}
	}
	return false
}

func validateWireguardKey(key string) error {
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil {
		return fmt.Errorf("wireguard key is not valid base64")
	}
	if len(raw) != 32 {
		return fmt.Errorf("wireguard key must decode to 32 bytes")
	}
	return nil
}

func validateCIDRList(field string, entries []string) error {
	for _, entry := range entries {
		if _, err := netip.ParsePrefix(entry); err != nil {
			return fmt.Errorf("%s: %q is not a valid CIDR (e.g. 10.64.222.21/32)", field, entry)
		}
	}
	return nil
}

func validateLocationList(field string, entries []string) error {
	for _, entry := range entries {
		if !locationRe.MatchString(entry) {
			return fmt.Errorf("%s: %q contains unsupported characters", field, entry)
		}
	}
	return nil
}

// validateConfig checks a complete configuration. All values end up in an
// env file consumed by docker compose, so everything is allow-list validated.
func validateConfig(c *Config) error {
	if c.Provider == "" {
		return fmt.Errorf("Provider is required")
	}
	if !isValidProvider(c.Provider) {
		return fmt.Errorf("unknown provider %q, see /providers", c.Provider)
	}

	switch c.VPNType {
	case "wireguard":
		if c.WireguardPrivateKey == "" {
			return fmt.Errorf("WireguardPrivateKey is required for wireguard")
		}
		if err := validateWireguardKey(c.WireguardPrivateKey); err != nil {
			return fmt.Errorf("WireguardPrivateKey: %v", err)
		}
		if c.WireguardPresharedKey != "" {
			if err := validateWireguardKey(c.WireguardPresharedKey); err != nil {
				return fmt.Errorf("WireguardPresharedKey: %v", err)
			}
		}
		if len(c.WireguardAddresses) == 0 {
			return fmt.Errorf("WireguardAddresses is required for wireguard")
		}
		if err := validateCIDRList("WireguardAddresses", c.WireguardAddresses); err != nil {
			return err
		}
		if c.Provider == "custom" {
			if c.WireguardPublicKey == "" || c.WireguardEndpointIP == "" || c.WireguardEndpointPort == 0 {
				return fmt.Errorf("custom wireguard needs WireguardPublicKey, WireguardEndpointIP and WireguardEndpointPort")
			}
			if err := validateWireguardKey(c.WireguardPublicKey); err != nil {
				return fmt.Errorf("WireguardPublicKey: %v", err)
			}
			if _, err := netip.ParseAddr(c.WireguardEndpointIP); err != nil {
				return fmt.Errorf("WireguardEndpointIP is not a valid IP")
			}
			if c.WireguardEndpointPort < 1 || c.WireguardEndpointPort > 65535 {
				return fmt.Errorf("WireguardEndpointPort must be 1-65535")
			}
		}
	case "openvpn":
		if c.OpenVPNUser == "" || c.OpenVPNPassword == "" {
			return fmt.Errorf("OpenVPNUser and OpenVPNPassword are required for openvpn")
		}
		if !credentialRe.MatchString(c.OpenVPNUser) {
			return fmt.Errorf("OpenVPNUser contains unsupported characters")
		}
		if !credentialRe.MatchString(c.OpenVPNPassword) {
			return fmt.Errorf("OpenVPNPassword contains unsupported characters")
		}
	default:
		return fmt.Errorf("VPNType must be one of %v", validVPNTypes())
	}

	if err := validateLocationList("ServerCountries", c.ServerCountries); err != nil {
		return err
	}
	if err := validateLocationList("ServerCities", c.ServerCities); err != nil {
		return err
	}

	if c.DNSAddress != "" {
		if _, err := netip.ParseAddr(c.DNSAddress); err != nil {
			return fmt.Errorf("DNSAddress is not a valid IP address")
		}
	}

	if err := validateCIDRList("FirewallOutboundSubnets", c.FirewallOutboundSubnets); err != nil {
		return err
	}

	return nil
}

func (c *Config) redacted() RedactedConfig {
	return RedactedConfig{
		Provider:                 c.Provider,
		VPNType:                  c.VPNType,
		WireguardKeySet:          c.WireguardPrivateKey != "",
		WireguardPresharedKeySet: c.WireguardPresharedKey != "",
		WireguardAddresses:       c.WireguardAddresses,
		WireguardPublicKey:       c.WireguardPublicKey,
		WireguardEndpointIP:      c.WireguardEndpointIP,
		WireguardEndpointPort:    c.WireguardEndpointPort,
		OpenVPNUser:              c.OpenVPNUser,
		OpenVPNPasswordSet:       c.OpenVPNPassword != "",
		ServerCountries:          c.ServerCountries,
		ServerCities:             c.ServerCities,
		DNSAddress:               c.DNSAddress,
		DisableDNSOverTLS:        c.DisableDNSOverTLS,
		FirewallOutboundSubnets:  c.FirewallOutboundSubnets,
		Configured:               validateConfig(c) == nil,
	}
}

// renderGluetunEnv produces the env file for the gluetun compose service.
// Every value has passed the allow-lists in validateConfig, so no value can
// contain newlines, quotes or dotenv/compose metacharacters.
func renderGluetunEnv(c *Config) string {
	var b strings.Builder
	b.WriteString("# Generated by spr-gluetun. Do not edit; use the plugin UI/API.\n")
	fmt.Fprintf(&b, "VPN_SERVICE_PROVIDER=%s\n", c.Provider)
	fmt.Fprintf(&b, "VPN_TYPE=%s\n", c.VPNType)

	if c.VPNType == "wireguard" {
		fmt.Fprintf(&b, "WIREGUARD_PRIVATE_KEY=%s\n", c.WireguardPrivateKey)
		if c.WireguardPresharedKey != "" {
			fmt.Fprintf(&b, "WIREGUARD_PRESHARED_KEY=%s\n", c.WireguardPresharedKey)
		}
		fmt.Fprintf(&b, "WIREGUARD_ADDRESSES=%s\n", strings.Join(c.WireguardAddresses, ","))
		if c.Provider == "custom" {
			fmt.Fprintf(&b, "WIREGUARD_PUBLIC_KEY=%s\n", c.WireguardPublicKey)
			fmt.Fprintf(&b, "WIREGUARD_ENDPOINT_IP=%s\n", c.WireguardEndpointIP)
			fmt.Fprintf(&b, "WIREGUARD_ENDPOINT_PORT=%d\n", c.WireguardEndpointPort)
		}
	} else {
		fmt.Fprintf(&b, "OPENVPN_USER=%s\n", c.OpenVPNUser)
		fmt.Fprintf(&b, "OPENVPN_PASSWORD=%s\n", c.OpenVPNPassword)
	}

	if len(c.ServerCountries) > 0 {
		fmt.Fprintf(&b, "SERVER_COUNTRIES=%s\n", strings.Join(c.ServerCountries, ","))
	}
	if len(c.ServerCities) > 0 {
		fmt.Fprintf(&b, "SERVER_CITIES=%s\n", strings.Join(c.ServerCities, ","))
	}

	// DNS over TLS through the tunnel (gluetun default resolver unless overridden)
	if c.DisableDNSOverTLS {
		b.WriteString("DOT=off\n")
	} else {
		b.WriteString("DOT=on\n")
	}
	if c.DNSAddress != "" {
		fmt.Fprintf(&b, "DNS_ADDRESS=%s\n", c.DNSAddress)
	}

	// hardening: killswitch always on, no proxy services, control server on
	// the compose network only (never published to the host)
	b.WriteString("FIREWALL=on\n")
	if len(c.FirewallOutboundSubnets) > 0 {
		fmt.Fprintf(&b, "FIREWALL_OUTBOUND_SUBNETS=%s\n", strings.Join(c.FirewallOutboundSubnets, ","))
	}
	b.WriteString("HTTPPROXY=off\n")
	b.WriteString("SHADOWSOCKS=off\n")
	b.WriteString("HTTP_CONTROL_SERVER_ADDRESS=:8000\n")

	return b.String()
}

// renderAuthConfig produces gluetun's control-server role config (v3.39+),
// restricted to exactly the routes this plugin uses.
func renderAuthConfig(apiKey string) string {
	var b strings.Builder
	b.WriteString("# Generated by spr-gluetun. Do not edit.\n")
	b.WriteString("[[roles]]\n")
	b.WriteString("name = \"spr-gluetun\"\n")
	routes := []string{
		"GET /v1/publicip/ip",
		"GET /v1/vpn/status",
		"PUT /v1/vpn/status",
	}
	sort.Strings(routes)
	quoted := make([]string, 0, len(routes))
	for _, route := range routes {
		quoted = append(quoted, fmt.Sprintf("%q", route))
	}
	fmt.Fprintf(&b, "routes = [%s]\n", strings.Join(quoted, ", "))
	b.WriteString("auth = \"apikey\"\n")
	fmt.Fprintf(&b, "apikey = %q\n", apiKey)
	return b.String()
}

func generateAPIKey() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// writeFileAtomicPrivate writes tmp+rename with 0600 (secrets stay private).
func writeFileAtomicPrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func loadConfig() error {
	Configmtx.Lock()
	defer Configmtx.Unlock()
	data, err := os.ReadFile(ConfigFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(data, &gConfig)
}

func writeConfigLocked() error {
	data, err := json.MarshalIndent(gConfig, "", " ")
	if err != nil {
		return err
	}
	return writeFileAtomicPrivate(ConfigFile, data)
}

// writeGluetunFilesLocked syncs the gluetun env file (when the config is
// complete) and the control-server auth config.
func writeGluetunFilesLocked() error {
	if gConfig.ControlAPIKey != "" {
		if err := writeFileAtomicPrivate(GluetunAuthFile, []byte(renderAuthConfig(gConfig.ControlAPIKey))); err != nil {
			return err
		}
	}
	if validateConfig(&gConfig) == nil {
		if err := writeFileAtomicPrivate(GluetunEnvFile, []byte(renderGluetunEnv(&gConfig))); err != nil {
			return err
		}
	}
	return nil
}

// ensureControlAPIKey generates the gluetun control-server API key on first
// run and persists it (0600) alongside gluetun's auth config.
func ensureControlAPIKey() error {
	Configmtx.Lock()
	defer Configmtx.Unlock()
	if gConfig.ControlAPIKey == "" {
		key, err := generateAPIKey()
		if err != nil {
			return err
		}
		gConfig.ControlAPIKey = key
		if err := writeConfigLocked(); err != nil {
			return err
		}
	}
	return writeGluetunFilesLocked()
}
