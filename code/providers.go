package main

// Provider is one entry of the static curated list served by GET /providers.
// Names are the exact VPN_SERVICE_PROVIDER values gluetun expects.
type Provider struct {
	Name     string   // gluetun VPN_SERVICE_PROVIDER value
	Label    string   // display name
	VPNTypes []string // supported VPN types in gluetun
}

// Providers is a curated subset of gluetun-supported providers
// (https://github.com/qdm12/gluetun-wiki/tree/main/setup/providers).
var Providers = []Provider{
	{Name: "airvpn", Label: "AirVPN", VPNTypes: []string{"wireguard", "openvpn"}},
	{Name: "cyberghost", Label: "CyberGhost", VPNTypes: []string{"openvpn"}},
	{Name: "expressvpn", Label: "ExpressVPN", VPNTypes: []string{"openvpn"}},
	{Name: "fastestvpn", Label: "FastestVPN", VPNTypes: []string{"wireguard", "openvpn"}},
	{Name: "hidemyass", Label: "HideMyAss", VPNTypes: []string{"openvpn"}},
	{Name: "ipvanish", Label: "IPVanish", VPNTypes: []string{"openvpn"}},
	{Name: "ivpn", Label: "IVPN", VPNTypes: []string{"wireguard", "openvpn"}},
	{Name: "mullvad", Label: "Mullvad", VPNTypes: []string{"wireguard"}},
	{Name: "nordvpn", Label: "NordVPN", VPNTypes: []string{"wireguard", "openvpn"}},
	{Name: "privado", Label: "Privado", VPNTypes: []string{"openvpn"}},
	{Name: "private internet access", Label: "Private Internet Access", VPNTypes: []string{"openvpn"}},
	{Name: "privatevpn", Label: "PrivateVPN", VPNTypes: []string{"openvpn"}},
	{Name: "protonvpn", Label: "Proton VPN", VPNTypes: []string{"wireguard", "openvpn"}},
	{Name: "purevpn", Label: "PureVPN", VPNTypes: []string{"openvpn"}},
	{Name: "surfshark", Label: "Surfshark", VPNTypes: []string{"wireguard", "openvpn"}},
	{Name: "torguard", Label: "TorGuard", VPNTypes: []string{"wireguard", "openvpn"}},
	{Name: "vyprvpn", Label: "VyprVPN", VPNTypes: []string{"openvpn"}},
	{Name: "windscribe", Label: "Windscribe", VPNTypes: []string{"wireguard", "openvpn"}},
	{Name: "custom", Label: "Custom (own WireGuard/OpenVPN server)", VPNTypes: []string{"wireguard", "openvpn"}},
}
