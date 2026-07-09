#!/bin/bash
# Command line install alternative to the SPR UI plugin installer.
set -euo pipefail
cd "$(dirname "$0")"

echo "Please enter your SPR path (/home/spr/super/)"
read -r SUPERDIR

if [ -z "$SUPERDIR" ]; then
    SUPERDIR="/home/spr/super/"
fi

export SUPERDIR

echo "Please enter your SPR API token:"
read -r SPR_API_TOKEN

if [ -z "$SPR_API_TOKEN" ]; then
  echo "need api token, generate one on the auth keys page"
  exit 1
fi

CONFIG_DIR="$SUPERDIR/configs/plugins/spr-gluetun"
STATE_DIR="$SUPERDIR/state/plugins/spr-gluetun"

mkdir -p "$CONFIG_DIR" "$STATE_DIR/gluetun"

# token for the plugin (matches plugin.json InstallTokenPath)
printf '%s' "$SPR_API_TOKEN" > "$CONFIG_DIR/api-token"
chmod 600 "$CONFIG_DIR/api-token"

# sourced by scripts/startup.sh
echo "SPR_API_TOKEN=$SPR_API_TOKEN" > "$CONFIG_DIR/config.sh"
chmod 600 "$CONFIG_DIR/config.sh"

# start with an empty plugin config; VPN settings are entered in the UI
# (or PUT to /plugins/spr-gluetun/config). The backend generates the gluetun
# control-server API key and writes gluetun.env + auth config on startup.
if [ ! -f "$CONFIG_DIR/config.json" ]; then
  echo '{}' > "$CONFIG_DIR/config.json"
  chmod 600 "$CONFIG_DIR/config.json"
fi

docker compose build
docker compose up -d

# Register the gluetun container IP as a custom interface so SPR grants it
# wan+dns egress and puts it in the vpn-egress group. Devices placed in the
# vpn-egress group can then be routed via the gluetun container IP.
GLUETUN_IP=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "spr-gluetun-vpn")
API=127.0.0.1

curl "http://${API}/firewall/custom_interface" \
-H "Authorization: Bearer ${SPR_API_TOKEN}" \
-X 'PUT' \
--data-raw "{\"SrcIP\":\"${GLUETUN_IP}\",\"Interface\":\"spr-gluetun\",\"Policies\":[\"wan\",\"dns\"],\"Groups\":[\"vpn-egress\"]}"

echo ""
echo "[+] spr-gluetun installed. Configure your VPN provider in the SPR UI"
echo "    (Plugins > spr-gluetun), then restart the plugin to apply."
