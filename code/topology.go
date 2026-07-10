package main

import "net/http"

// GluetunContainerIP is the fixed address of the gluetun container on the
// plugin's docker bridge (see the ipam config in docker-compose.yml). SPR
// devices in the vpn-egress group are routed to this IP.
var GluetunContainerIP = "172.30.117.2"

// TopoNode / TopoEdge / Topology mirror the shapes SPR expects from plugin
// topology endpoints (same contract as spr-tailscale). The SPR host merges
// the plugin graph into the router topology at the "root" anchor node.
type TopoNode struct {
	ID       string
	Kind     string
	Name     string
	IP       string `json:",omitempty"`
	ConnType string `json:",omitempty"`
	Online   bool
}

type TopoEdge struct {
	From  string
	To    string
	Layer string
	Kind  string
}

type Topology struct {
	Nodes []TopoNode
	Edges []TopoEdge
}

// topologyData is the live state the graph builder consumes, separated from
// the gluetun control-server client so tests can drive it directly.
type topologyData struct {
	VPNType          string // "wireguard" or "openvpn" (from config)
	ControlReachable bool   // gluetun control server responded
	TunnelRunning    bool   // VPN status == "running"
	PublicIP         string
	City             string
	Country          string
}

// exitNodeName renders the VPN exit label from public-ip data.
func exitNodeName(city, country string) string {
	switch {
	case city != "" && country != "":
		return city + ", " + country
	case country != "":
		return country
	case city != "":
		return city
	}
	return "VPN exit"
}

// buildTopology renders the plugin subgraph:
//
//	root ──(vpn/bridge)── gateway ──(vpn/<type>)── vpn-exit
//
// The gateway node is always present (its Online flag reflects control-server
// reachability); the vpn-exit node exists only while the tunnel is running.
func buildTopology(d topologyData) Topology {
	connType := d.VPNType
	if connType == "" {
		connType = "wireguard"
	}

	topo := Topology{
		Nodes: []TopoNode{{ID: "root", ConnType: connType, Online: true}},
		Edges: []TopoEdge{},
	}

	topo.Nodes = append(topo.Nodes, TopoNode{
		ID:       "gluetun",
		Kind:     "gateway",
		Name:     "Gluetun gateway",
		IP:       GluetunContainerIP,
		ConnType: connType,
		Online:   d.ControlReachable,
	})
	topo.Edges = append(topo.Edges, TopoEdge{From: "root", To: "gluetun", Layer: "vpn", Kind: "bridge"})

	if d.TunnelRunning {
		topo.Nodes = append(topo.Nodes, TopoNode{
			ID:       "vpn-exit",
			Kind:     "vpn-exit",
			Name:     exitNodeName(d.City, d.Country),
			IP:       d.PublicIP,
			ConnType: connType,
			Online:   true,
		})
		topo.Edges = append(topo.Edges, TopoEdge{From: "gluetun", To: "vpn-exit", Layer: "vpn", Kind: connType})
	}

	return topo
}

// GET /topology — the plugin's live subgraph for SPR's topology view.
func handleGetTopology(w http.ResponseWriter, r *http.Request) {
	Configmtx.RLock()
	d := topologyData{VPNType: gConfig.VPNType}
	Configmtx.RUnlock()

	if status, err := getVPNStatus(); err == nil {
		d.ControlReachable = true
		d.TunnelRunning = status == "running"
	}

	if d.TunnelRunning {
		if ip, err := getPublicIP(); err == nil {
			d.PublicIP = ip.PublicIP
			d.City = ip.City
			d.Country = ip.Country
		}
	}

	jsonResponse(w, buildTopology(d))
}
