package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

var UNIX_PLUGIN_LISTENER = "/run/spr-krun-plugin/spr-gluetun.sock"

type StatusResponse struct {
	Configured       bool
	ControlReachable bool
	VPNStatus        string
	GatewayIP        string
	PublicIP         string `json:",omitempty"`
	Region           string `json:",omitempty"`
	Country          string `json:",omitempty"`
	City             string `json:",omitempty"`
}

func jsonResponse(w http.ResponseWriter, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		fmt.Println("failed to encode response:", err)
	}
}

// GET /status — VPN + public IP state from gluetun's control server.
func handleGetStatus(w http.ResponseWriter, r *http.Request) {
	Configmtx.RLock()
	configured := validateConfig(&gConfig) == nil
	Configmtx.RUnlock()

	response := StatusResponse{
		Configured: configured,
		VPNStatus:  "unknown",
		GatewayIP:  GluetunContainerIP,
	}

	if status, err := getVPNStatus(); err == nil {
		response.ControlReachable = true
		response.VPNStatus = status
	} else {
		fmt.Println("gluetun control server unreachable:", err)
		jsonResponse(w, response)
		return
	}

	if response.VPNStatus == "running" {
		if ip, err := getPublicIP(); err == nil {
			response.PublicIP = ip.PublicIP
			response.Region = ip.Region
			response.Country = ip.Country
			response.City = ip.City
		} else {
			fmt.Println("failed to get public ip:", err)
		}
	}

	jsonResponse(w, response)
}

// GET /config — redacted view, secrets are never returned.
func handleGetConfig(w http.ResponseWriter, r *http.Request) {
	Configmtx.RLock()
	defer Configmtx.RUnlock()
	jsonResponse(w, gConfig.redacted())
}

// PUT /config — validate, persist (0600) and regenerate gluetun.env + the
// control-server auth config. Secret fields left empty keep their previous
// value so the UI can update filters without re-entering keys.
func handlePutConfig(w http.ResponseWriter, r *http.Request) {
	incoming := Config{}
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	Configmtx.Lock()
	defer Configmtx.Unlock()

	// write-only secrets: empty means "keep existing"
	if incoming.WireguardPrivateKey == "" {
		incoming.WireguardPrivateKey = gConfig.WireguardPrivateKey
	}
	if incoming.WireguardPresharedKey == "" {
		incoming.WireguardPresharedKey = gConfig.WireguardPresharedKey
	}
	if incoming.OpenVPNPassword == "" {
		incoming.OpenVPNPassword = gConfig.OpenVPNPassword
	}
	// never accepted from clients
	incoming.ControlAPIKey = gConfig.ControlAPIKey

	if err := validateConfig(&incoming); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	gConfig = incoming
	if err := writeConfigLocked(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if err := writeGluetunFilesLocked(); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	jsonResponse(w, gConfig.redacted())
}

type vpnRequest struct {
	Status string // "running" or "stopped"
}

// PUT /vpn — start/stop the tunnel via gluetun's control server.
func handlePutVPN(w http.ResponseWriter, r *http.Request) {
	req := vpnRequest{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	if req.Status != "running" && req.Status != "stopped" {
		http.Error(w, "Status must be \"running\" or \"stopped\"", 400)
		return
	}
	if err := setVPNStatus(req.Status); err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	jsonResponse(w, map[string]string{"Status": req.Status})
}

// POST /restart — bounce the tunnel (stop, then start) inside the running
// gluetun container. Provider/credential changes are picked up from
// gluetun.env only on container recreation: toggle the plugin in SPR or run
// `docker compose restart` for those.
func handleRestart(w http.ResponseWriter, r *http.Request) {
	if err := setVPNStatus("stopped"); err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	time.Sleep(2 * time.Second)
	if err := setVPNStatus("running"); err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	jsonResponse(w, map[string]string{"Status": "restarted"})
}

// GET /providers — static curated list of gluetun-supported providers.
func handleGetProviders(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, Providers)
}

type spaHandler struct {
	staticPath string
	indexPath  string
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path, err := filepath.Abs(r.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	path = filepath.Join(h.staticPath, path)
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		http.ServeFile(w, r, filepath.Join(h.staticPath, h.indexPath))
		return
	} else if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.FileServer(http.Dir(h.staticPath)).ServeHTTP(w, r)
}

func logRequest(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Printf("%s %s %s\n", r.RemoteAddr, r.Method, r.URL)
		handler.ServeHTTP(w, r)
	})
}

func main() {
	GluetunContainerIP = containerIPv4()

	if err := loadConfig(); err != nil {
		fmt.Println("failed to load config:", err)
	}

	// first run: generate the gluetun control-server API key, write the auth
	// config (0600) and, if configured, the gluetun env file
	if err := ensureControlAPIKey(); err != nil {
		fmt.Println("failed to initialize gluetun control config:", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", handleGetStatus)
	mux.HandleFunc("GET /config", handleGetConfig)
	mux.HandleFunc("PUT /config", handlePutConfig)
	mux.HandleFunc("PUT /vpn", handlePutVPN)
	mux.HandleFunc("POST /restart", handleRestart)
	mux.HandleFunc("GET /providers", handleGetProviders)
	mux.HandleFunc("GET /topology", handleGetTopology)

	// serve the bundled UI for everything else
	mux.Handle("/", spaHandler{staticPath: "/ui", indexPath: "index.html"})

	os.Remove(UNIX_PLUGIN_LISTENER)
	listener, err := net.Listen("unix", UNIX_PLUGIN_LISTENER)
	if err != nil {
		panic(err)
	}
	if err := os.Chmod(UNIX_PLUGIN_LISTENER, 0770); err != nil {
		panic(err)
	}

	server := http.Server{Handler: logRequest(mux)}
	if err := server.Serve(listener); err != nil {
		panic(err)
	}
}
