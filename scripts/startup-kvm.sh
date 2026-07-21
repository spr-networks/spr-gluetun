#!/bin/sh
set -eu

set -a
. /configs/base/config.sh
if [ -f /configs/spr-gluetun/config.sh ]; then
    . /configs/spr-gluetun/config.sh
fi
set +a

if [ -z "${SPR_KRUN_VSOCK_PORT:-}" ] || [ -z "${SPR_KRUN_PLUGIN_SOCKET:-}" ]; then
    echo "SPR_KRUN_VSOCK_PORT and SPR_KRUN_PLUGIN_SOCKET must be set" >&2
    exit 64
fi

mkdir -p "$(dirname "$SPR_KRUN_PLUGIN_SOCKET")"
/usr/local/bin/spr-krun-vsock-proxy \
    -port "$SPR_KRUN_VSOCK_PORT" \
    -socket "$SPR_KRUN_PLUGIN_SOCKET" &
proxy_pid=$!

/spr_gluetun_plugin &
plugin_pid=$!
gluetun_pid=""

stop_all() {
    kill "$proxy_pid" "$plugin_pid" ${gluetun_pid:+"$gluetun_pid"} 2>/dev/null || true
}
trap stop_all INT TERM HUP

while [ ! -f /configs/spr-gluetun/gluetun.env ]; do
    if ! kill -0 "$plugin_pid" 2>/dev/null; then
        wait "$plugin_pid"
        exit $?
    fi
    sleep 1
done

set -a
. /configs/spr-gluetun/gluetun.env
set +a
/gluetun-entrypoint &
gluetun_pid=$!

set +e
wait -n "$plugin_pid" "$gluetun_pid"
status=$?
stop_all
wait "$plugin_pid" "$gluetun_pid" 2>/dev/null || true
exit "$status"
