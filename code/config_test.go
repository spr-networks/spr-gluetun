package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// valid base64 of 32 bytes (a wireguard-shaped key, not a real secret)
const testWGKey = "GHU4NIobLkS0puTSeCEUnj/2X1zTNP1WhQLcLdBQ1V0="

func validWireguardConfig() Config {
	return Config{
		Provider:            "mullvad",
		VPNType:             "wireguard",
		WireguardPrivateKey: testWGKey,
		WireguardAddresses:  []string{"10.64.222.21/32"},
		ServerCountries:     []string{"Netherlands"},
	}
}

func validOpenVPNConfig() Config {
	return Config{
		Provider:        "protonvpn",
		VPNType:         "openvpn",
		OpenVPNUser:     "someuser",
		OpenVPNPassword: "some pass_word.123",
		ServerCountries: []string{"United States"},
		ServerCities:    []string{"New York"},
	}
}

func TestValidateConfigWireguard(t *testing.T) {
	cfg := validWireguardConfig()
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("expected valid wireguard config, got %v", err)
	}
}

func TestValidateConfigOpenVPN(t *testing.T) {
	cfg := validOpenVPNConfig()
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("expected valid openvpn config, got %v", err)
	}
}

func TestValidateConfigRejects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"empty provider", func(c *Config) { c.Provider = "" }},
		{"unknown provider", func(c *Config) { c.Provider = "notaprovider" }},
		{"bad vpn type", func(c *Config) { c.VPNType = "ipsec" }},
		{"missing wg key", func(c *Config) { c.WireguardPrivateKey = "" }},
		{"non-base64 wg key", func(c *Config) { c.WireguardPrivateKey = "not-base64!!!" }},
		{"short wg key", func(c *Config) { c.WireguardPrivateKey = "aGVsbG8=" }},
		{"missing wg addresses", func(c *Config) { c.WireguardAddresses = nil }},
		{"bad wg address", func(c *Config) { c.WireguardAddresses = []string{"10.0.0.1"} }},
		{"newline in country", func(c *Config) { c.ServerCountries = []string{"Nether\nlands"} }},
		{"env injection in country", func(c *Config) { c.ServerCountries = []string{"x\nFIREWALL=off"} }},
		{"bad dns", func(c *Config) { c.DNSAddress = "not-an-ip" }},
		{"bad outbound subnet", func(c *Config) { c.FirewallOutboundSubnets = []string{"192.168.2.0"} }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validWireguardConfig()
			tc.mutate(&cfg)
			if err := validateConfig(&cfg); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestValidateConfigRejectsOpenVPN(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
	}{
		{"missing user", func(c *Config) { c.OpenVPNUser = "" }},
		{"missing password", func(c *Config) { c.OpenVPNPassword = "" }},
		{"quote in password", func(c *Config) { c.OpenVPNPassword = `pass"word` }},
		{"dollar in password", func(c *Config) { c.OpenVPNPassword = "pa$$word" }},
		{"hash in password", func(c *Config) { c.OpenVPNPassword = "pass#word" }},
		{"backslash in user", func(c *Config) { c.OpenVPNUser = `dom\user` }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validOpenVPNConfig()
			tc.mutate(&cfg)
			if err := validateConfig(&cfg); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestValidateCustomWireguard(t *testing.T) {
	cfg := validWireguardConfig()
	cfg.Provider = "custom"
	if err := validateConfig(&cfg); err == nil {
		t.Fatal("custom provider without endpoint fields should fail")
	}

	cfg.WireguardPublicKey = testWGKey
	cfg.WireguardEndpointIP = "203.0.113.10"
	cfg.WireguardEndpointPort = 51820
	if err := validateConfig(&cfg); err != nil {
		t.Fatalf("expected valid custom wireguard config, got %v", err)
	}
}

func TestRenderGluetunEnvWireguard(t *testing.T) {
	cfg := validWireguardConfig()
	env := renderGluetunEnv(&cfg)

	for _, want := range []string{
		"VPN_SERVICE_PROVIDER=mullvad\n",
		"VPN_TYPE=wireguard\n",
		"WIREGUARD_PRIVATE_KEY=" + testWGKey + "\n",
		"WIREGUARD_ADDRESSES=10.64.222.21/32\n",
		"SERVER_COUNTRIES=Netherlands\n",
		// hardening must always be present
		"FIREWALL=on\n",
		"DOT=on\n",
		"HTTPPROXY=off\n",
		"SHADOWSOCKS=off\n",
		"HTTP_CONTROL_SERVER_ADDRESS=:8000\n",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %q:\n%s", want, env)
		}
	}

	if strings.Contains(env, "OPENVPN") {
		t.Error("wireguard env must not contain openvpn settings")
	}
}

func TestRenderGluetunEnvOpenVPN(t *testing.T) {
	cfg := validOpenVPNConfig()
	cfg.DisableDNSOverTLS = true
	cfg.FirewallOutboundSubnets = []string{"192.168.2.0/24"}
	env := renderGluetunEnv(&cfg)

	for _, want := range []string{
		"VPN_TYPE=openvpn\n",
		"OPENVPN_USER=someuser\n",
		"OPENVPN_PASSWORD=some pass_word.123\n",
		"SERVER_CITIES=New York\n",
		"DOT=off\n",
		"FIREWALL=on\n",
		"FIREWALL_OUTBOUND_SUBNETS=192.168.2.0/24\n",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("env missing %q:\n%s", want, env)
		}
	}

	if strings.Contains(env, "WIREGUARD") {
		t.Error("openvpn env must not contain wireguard settings")
	}
}

func TestRenderAuthConfig(t *testing.T) {
	out := renderAuthConfig("deadbeef")
	for _, want := range []string{
		"[[roles]]",
		`auth = "apikey"`,
		`apikey = "deadbeef"`,
		`"GET /v1/publicip/ip"`,
		`"GET /v1/vpn/status"`,
		`"PUT /v1/vpn/status"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("auth config missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "/v1/vpn/settings") {
		t.Error("auth config must not grant access to VPN settings (contains secrets)")
	}
}

func TestGenerateAPIKey(t *testing.T) {
	k1, err := generateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	k2, err := generateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(k1) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(k1))
	}
	if k1 == k2 {
		t.Fatal("api keys must be random")
	}
}

func TestRedactedConfigHidesSecrets(t *testing.T) {
	cfg := validWireguardConfig()
	cfg.WireguardPresharedKey = testWGKey
	cfg.OpenVPNPassword = "supersecret"
	cfg.ControlAPIKey = "controlkey123"

	data, err := json.Marshal(cfg.redacted())
	if err != nil {
		t.Fatal(err)
	}
	out := string(data)

	for _, secret := range []string{testWGKey, "supersecret", "controlkey123"} {
		if strings.Contains(out, secret) {
			t.Errorf("redacted config leaks secret %q: %s", secret, out)
		}
	}

	red := cfg.redacted()
	if !red.WireguardKeySet || !red.WireguardPresharedKeySet {
		t.Error("redacted config should flag configured secrets as set")
	}
	if !red.Configured {
		t.Error("valid config should report Configured=true")
	}
}

func TestConfigFileRoundtrip(t *testing.T) {
	dir := t.TempDir()

	oldConfig, oldEnv, oldAuth := ConfigFile, GluetunEnvFile, GluetunAuthFile
	defer func() { ConfigFile, GluetunEnvFile, GluetunAuthFile = oldConfig, oldEnv, oldAuth }()
	ConfigFile = filepath.Join(dir, "config.json")
	GluetunEnvFile = filepath.Join(dir, "gluetun.env")
	GluetunAuthFile = filepath.Join(dir, "auth", "config.toml")

	Configmtx.Lock()
	oldG := gConfig
	gConfig = validWireguardConfig()
	gConfig.ControlAPIKey = "cafef00d"
	if err := writeConfigLocked(); err != nil {
		t.Fatal(err)
	}
	if err := writeGluetunFilesLocked(); err != nil {
		t.Fatal(err)
	}
	gConfig = oldG
	Configmtx.Unlock()

	for _, f := range []string{ConfigFile, GluetunEnvFile, GluetunAuthFile} {
		info, err := os.Stat(f)
		if err != nil {
			t.Fatalf("expected %s to exist: %v", f, err)
		}
		if perm := info.Mode().Perm(); perm != 0600 {
			t.Errorf("%s should be 0600, got %o", f, perm)
		}
	}

	if err := loadConfig(); err != nil {
		t.Fatal(err)
	}
	Configmtx.RLock()
	defer Configmtx.RUnlock()
	if gConfig.Provider != "mullvad" || gConfig.ControlAPIKey != "cafef00d" {
		t.Errorf("config did not roundtrip: %+v", gConfig)
	}
}
