package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// gluetun's control server, reachable only on the plugin's own docker bridge
// ("gluetun" is the compose service name). Never published on the host.
var GluetunControlURL = "http://gluetun:8000"

var gluetunHTTP = &http.Client{Timeout: 5 * time.Second}

// controlRequest performs an authenticated request against gluetun's control
// server (v3.39+ requires an API key; ours is generated at first run).
func controlRequest(method, path string, body interface{}, out interface{}) error {
	Configmtx.RLock()
	apiKey := gConfig.ControlAPIKey
	Configmtx.RUnlock()

	if apiKey == "" {
		return fmt.Errorf("control server API key not initialized")
	}

	var reqBody *bytes.Buffer = bytes.NewBuffer(nil)
	if body != nil {
		if err := json.NewEncoder(reqBody).Encode(body); err != nil {
			return err
		}
	}

	req, err := http.NewRequest(method, GluetunControlURL+path, reqBody)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := gluetunHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("gluetun control server: %s %s -> %s", method, path, resp.Status)
	}

	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type gluetunVPNStatus struct {
	Status string `json:"status"`
}

type gluetunPublicIP struct {
	PublicIP string `json:"public_ip"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	City     string `json:"city"`
}

func getVPNStatus() (string, error) {
	status := gluetunVPNStatus{}
	if err := controlRequest(http.MethodGet, "/v1/vpn/status", nil, &status); err != nil {
		return "", err
	}
	return status.Status, nil
}

// setVPNStatus starts/stops the tunnel inside the running gluetun container
// via PUT /v1/vpn/status. status must be "running" or "stopped".
func setVPNStatus(status string) error {
	if status != "running" && status != "stopped" {
		return fmt.Errorf("status must be \"running\" or \"stopped\"")
	}
	return controlRequest(http.MethodPut, "/v1/vpn/status", gluetunVPNStatus{Status: status}, nil)
}

func getPublicIP() (gluetunPublicIP, error) {
	ip := gluetunPublicIP{}
	err := controlRequest(http.MethodGet, "/v1/publicip/ip", nil, &ip)
	return ip, err
}
