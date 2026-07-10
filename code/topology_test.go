package main

import "testing"

func nodeByID(t *testing.T, topo Topology, id string) TopoNode {
	t.Helper()
	for _, n := range topo.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("node %q not found in %+v", id, topo.Nodes)
	return TopoNode{}
}

func TestBuildTopologyTunnelRunning(t *testing.T) {
	topo := buildTopology(topologyData{
		VPNType:          "wireguard",
		ControlReachable: true,
		TunnelRunning:    true,
		PublicIP:         "203.0.113.77",
		City:             "Amsterdam",
		Country:          "Netherlands",
	})

	if len(topo.Nodes) != 3 {
		t.Fatalf("expected 3 nodes (root, gateway, exit), got %d: %+v", len(topo.Nodes), topo.Nodes)
	}
	if len(topo.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d: %+v", len(topo.Edges), topo.Edges)
	}

	root := nodeByID(t, topo, "root")
	if root.ConnType != "wireguard" || !root.Online {
		t.Errorf("bad root anchor: %+v", root)
	}

	gw := nodeByID(t, topo, "gluetun")
	if gw.Kind != "gateway" || gw.IP != GluetunContainerIP || !gw.Online {
		t.Errorf("bad gateway node: %+v", gw)
	}

	exit := nodeByID(t, topo, "vpn-exit")
	if exit.Kind != "vpn-exit" || !exit.Online {
		t.Errorf("bad exit node: %+v", exit)
	}
	if exit.Name != "Amsterdam, Netherlands" {
		t.Errorf("exit name should be \"City, Country\", got %q", exit.Name)
	}
	if exit.IP != "203.0.113.77" {
		t.Errorf("exit IP should be the public IP, got %q", exit.IP)
	}

	if e := topo.Edges[0]; e.From != "root" || e.To != "gluetun" || e.Layer != "vpn" {
		t.Errorf("bad root->gateway edge: %+v", e)
	}
	if e := topo.Edges[1]; e.From != "gluetun" || e.To != "vpn-exit" || e.Layer != "vpn" || e.Kind != "wireguard" {
		t.Errorf("bad gateway->exit edge: %+v", e)
	}
}

func TestBuildTopologySink(t *testing.T) {
	topo := buildTopology(topologyData{ControlReachable: true, TunnelRunning: true})
	if len(topo.Sinks) != 1 {
		t.Fatalf("expected 1 sink, got %+v", topo.Sinks)
	}
	sink := topo.Sinks[0]
	if sink.ID != "gluetun" || sink.Iface != GluetunBridgeInterface ||
		sink.IP != GluetunContainerIP || !sink.Online {
		t.Errorf("bad sink advertisement: %+v", sink)
	}

	down := buildTopology(topologyData{ControlReachable: true, TunnelRunning: false})
	if len(down.Sinks) != 1 || down.Sinks[0].Online {
		t.Errorf("sink should be advertised but offline when tunnel is down: %+v", down.Sinks)
	}
}

func TestBuildTopologyTunnelStopped(t *testing.T) {
	topo := buildTopology(topologyData{
		VPNType:          "wireguard",
		ControlReachable: true,
		TunnelRunning:    false,
	})

	if len(topo.Nodes) != 2 {
		t.Fatalf("tunnel down should yield root+gateway only, got %+v", topo.Nodes)
	}
	if len(topo.Edges) != 1 {
		t.Fatalf("tunnel down should yield a single edge, got %+v", topo.Edges)
	}
	gw := nodeByID(t, topo, "gluetun")
	if !gw.Online {
		t.Error("gateway should be online while the control server is reachable")
	}
}

func TestBuildTopologyControlUnreachable(t *testing.T) {
	topo := buildTopology(topologyData{VPNType: "openvpn"})

	if len(topo.Nodes) != 2 || len(topo.Edges) != 1 {
		t.Fatalf("unreachable daemon should yield root+gateway, got %+v", topo)
	}
	gw := nodeByID(t, topo, "gluetun")
	if gw.Online {
		t.Error("gateway must be offline when the control server is unreachable")
	}
	for _, n := range topo.Nodes {
		if n.Kind == "vpn-exit" {
			t.Error("no exit node when the tunnel is not running")
		}
	}
}

func TestBuildTopologyConnTypeFromConfig(t *testing.T) {
	topo := buildTopology(topologyData{
		VPNType:          "openvpn",
		ControlReachable: true,
		TunnelRunning:    true,
		PublicIP:         "198.51.100.9",
		Country:          "Sweden",
	})

	if nodeByID(t, topo, "root").ConnType != "openvpn" {
		t.Error("root ConnType should follow the configured VPN type")
	}
	if e := topo.Edges[1]; e.Kind != "openvpn" {
		t.Errorf("tunnel edge kind should be the VPN type, got %q", e.Kind)
	}
	if got := nodeByID(t, topo, "vpn-exit").Name; got != "Sweden" {
		t.Errorf("exit name with country only should be %q, got %q", "Sweden", got)
	}
}

func TestBuildTopologyDefaults(t *testing.T) {
	// unconfigured plugin: no VPNType yet, daemon down
	topo := buildTopology(topologyData{})
	if nodeByID(t, topo, "root").ConnType != "wireguard" {
		t.Error("empty VPNType should default the root ConnType to wireguard")
	}

	// running tunnel but public-ip lookup failed: honest fallback label
	topo = buildTopology(topologyData{ControlReachable: true, TunnelRunning: true})
	if got := nodeByID(t, topo, "vpn-exit").Name; got != "VPN exit" {
		t.Errorf("missing location should fall back to %q, got %q", "VPN exit", got)
	}
}
