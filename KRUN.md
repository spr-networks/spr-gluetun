# krun branch status

The SPR control/UI backend runs in a krun microVM with a TAP identity and a
vsock-only plugin API. The upstream gluetun gateway remains a conventional
Docker service on `spr-gluetun`, because that bridge address is SPR's current
forwarding destination for the `vpn-glutun` group.

This is a split migration, not full isolation of the upstream VPN process.
Moving gluetun itself into the same microVM requires replacing the
container-to-container control connection and changing the advertised
forwarding destination to the VM's SPR DHCP address.
