#!/bin/bash
set -a
. /configs/base/config.sh
if [ -f /configs/spr-gluetun/config.sh ]; then
    . /configs/spr-gluetun/config.sh
fi
set +a

exec /spr_gluetun_plugin
