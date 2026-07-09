import React, { useCallback, useEffect, useRef, useState } from 'react'
import {
  api,
  useAlert,
  Page,
  ListHeader,
  Card,
  SectionHeader,
  StatTile,
  KeyVal,
  StatusDot,
  Toggle,
  TextField,
  ModalConfirm,
  Loading,
  Button,
  ButtonText,
  HStack,
  VStack,
  Text
} from '@spr-networks/plugin-ui'

const PLUGIN_BASE = `/plugins/${api.pluginURI() || 'spr-gluetun'}`

const GATEWAY_IP = '172.30.117.2'

const splitList = (value) =>
  (value || '')
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item.length)

const joinList = (items) => (items && items.length ? items.join(', ') : '')

const PillGroup = ({ options, value, onChange }) => (
  <HStack flexWrap="wrap" gap="$2">
    {options.map((option) => (
      <Button
        key={option.value}
        size="xs"
        variant={option.value === value ? 'solid' : 'outline'}
        onPress={() => onChange(option.value)}
      >
        <ButtonText>{option.label}</ButtonText>
      </Button>
    ))}
  </HStack>
)

export default function Plugin() {
  const alert = useAlert()
  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState(null)
  const [providers, setProviders] = useState([])
  const [config, setConfig] = useState(null)
  const [busy, setBusy] = useState(false)
  const [showStop, setShowStop] = useState(false)

  // form state (secrets start empty: they are write-only server-side)
  const [form, setForm] = useState({
    Provider: '',
    VPNType: 'wireguard',
    WireguardPrivateKey: '',
    WireguardPresharedKey: '',
    WireguardAddresses: '',
    WireguardPublicKey: '',
    WireguardEndpointIP: '',
    WireguardEndpointPort: '',
    OpenVPNUser: '',
    OpenVPNPassword: '',
    ServerCountries: '',
    ServerCities: '',
    DNSAddress: '',
    DNSOverTLS: true,
    FirewallOutboundSubnets: ''
  })

  const setField = (key) => (value) => setForm((f) => ({ ...f, [key]: value }))

  const statusTimer = useRef(null)

  const refreshStatus = useCallback(() => {
    api
      .get(`${PLUGIN_BASE}/status`)
      .then(setStatus)
      .catch(() => setStatus(null))
  }, [])

  const loadAll = useCallback(() => {
    Promise.all([
      api.get(`${PLUGIN_BASE}/config`),
      api.get(`${PLUGIN_BASE}/providers`),
      api.get(`${PLUGIN_BASE}/status`).catch(() => null)
    ])
      .then(([cfg, provs, stat]) => {
        setConfig(cfg)
        setProviders(provs || [])
        setStatus(stat)
        setForm((f) => ({
          ...f,
          Provider: cfg.Provider || '',
          VPNType: cfg.VPNType || 'wireguard',
          WireguardAddresses: joinList(cfg.WireguardAddresses),
          WireguardPublicKey: cfg.WireguardPublicKey || '',
          WireguardEndpointIP: cfg.WireguardEndpointIP || '',
          WireguardEndpointPort: cfg.WireguardEndpointPort
            ? String(cfg.WireguardEndpointPort)
            : '',
          OpenVPNUser: cfg.OpenVPNUser || '',
          ServerCountries: joinList(cfg.ServerCountries),
          ServerCities: joinList(cfg.ServerCities),
          DNSAddress: cfg.DNSAddress || '',
          DNSOverTLS: !cfg.DisableDNSOverTLS,
          FirewallOutboundSubnets: joinList(cfg.FirewallOutboundSubnets)
        }))
      })
      .catch((err) => alert.error('Failed to load plugin data', err))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    loadAll()
    statusTimer.current = setInterval(refreshStatus, 10000)
    return () => clearInterval(statusTimer.current)
  }, [])

  const save = () => {
    const port = parseInt(form.WireguardEndpointPort, 10)
    const payload = {
      Provider: form.Provider,
      VPNType: form.VPNType,
      WireguardPrivateKey: form.WireguardPrivateKey,
      WireguardPresharedKey: form.WireguardPresharedKey,
      WireguardAddresses: splitList(form.WireguardAddresses),
      WireguardPublicKey: form.WireguardPublicKey,
      WireguardEndpointIP: form.WireguardEndpointIP,
      WireguardEndpointPort: isNaN(port) ? 0 : port,
      OpenVPNUser: form.OpenVPNUser,
      OpenVPNPassword: form.OpenVPNPassword,
      ServerCountries: splitList(form.ServerCountries),
      ServerCities: splitList(form.ServerCities),
      DNSAddress: form.DNSAddress,
      DisableDNSOverTLS: !form.DNSOverTLS,
      FirewallOutboundSubnets: splitList(form.FirewallOutboundSubnets)
    }

    setBusy(true)
    api
      .put(`${PLUGIN_BASE}/config`, payload)
      .then((cfg) => {
        setConfig(cfg)
        setForm((f) => ({
          ...f,
          WireguardPrivateKey: '',
          WireguardPresharedKey: '',
          OpenVPNPassword: ''
        }))
        alert.success(
          'Saved. Restart the plugin (Plugins page) or run docker compose restart to apply provider changes.'
        )
      })
      .catch((err) => alert.error('Failed to save config', err))
      .finally(() => setBusy(false))
  }

  const setTunnel = (running) => {
    setBusy(true)
    api
      .put(`${PLUGIN_BASE}/vpn`, { Status: running ? 'running' : 'stopped' })
      .then(() => {
        alert.success(running ? 'Tunnel starting' : 'Tunnel stopped')
        setTimeout(refreshStatus, 1500)
      })
      .catch((err) => alert.error('Tunnel command failed', err))
      .finally(() => setBusy(false))
  }

  const restartTunnel = () => {
    setBusy(true)
    api
      .post(`${PLUGIN_BASE}/restart`)
      .then(() => {
        alert.success('Tunnel restarted')
        setTimeout(refreshStatus, 1500)
      })
      .catch((err) => alert.error('Restart failed', err))
      .finally(() => setBusy(false))
  }

  if (loading) {
    return (
      <Page>
        <Loading />
      </Page>
    )
  }

  const running = status?.VPNStatus === 'running'
  const reachable = !!status?.ControlReachable
  const provider = providers.find((p) => p.Name === form.Provider)
  const vpnTypes = provider?.VPNTypes || ['wireguard', 'openvpn']
  const isWireguard = form.VPNType === 'wireguard'
  const isCustom = form.Provider === 'custom'

  return (
    <Page>
      <ListHeader
        title="Gluetun VPN Gateway"
        description="Route SPR devices in the vpn-egress group through an outbound VPN tunnel"
        mark="gt"
        status={running ? 'Tunnel active' : reachable ? 'Stopped' : 'Unreachable'}
        statusAction={running ? 'success' : reachable ? 'warning' : 'error'}
      >
        <Button size="sm" isDisabled={busy} onPress={save}>
          <ButtonText>Save</ButtonText>
        </Button>
      </ListHeader>

      <Card>
        <SectionHeader
          title="Tunnel status"
          right={<StatusDot online={running} warn={reachable && !running} />}
        />
        <HStack flexWrap="wrap" gap="$2">
          <StatTile
            label="Tunnel"
            value={reachable ? status?.VPNStatus || 'unknown' : 'unreachable'}
          />
          <StatTile label="Public IP" value={status?.PublicIP || '—'} mono />
          <StatTile label="Country" value={status?.Country || '—'} />
          <StatTile label="City" value={status?.City || '—'} />
        </HStack>
        <VStack space="sm" mt="$2">
          <KeyVal label="Configured" value={status?.Configured ? 'yes' : 'no'} />
          <KeyVal label="Gateway IP (vpn-egress)" value={GATEWAY_IP} mono />
          <HStack gap="$2" mt="$2">
            {running ? (
              <Button
                size="xs"
                variant="outline"
                action="negative"
                isDisabled={busy || !reachable}
                onPress={() => setShowStop(true)}
              >
                <ButtonText>Stop tunnel</ButtonText>
              </Button>
            ) : (
              <Button
                size="xs"
                isDisabled={busy || !reachable}
                onPress={() => setTunnel(true)}
              >
                <ButtonText>Start tunnel</ButtonText>
              </Button>
            )}
            <Button
              size="xs"
              variant="outline"
              isDisabled={busy || !reachable}
              onPress={restartTunnel}
            >
              <ButtonText>Restart tunnel</ButtonText>
            </Button>
          </HStack>
          {!reachable ? (
            <Text size="xs" color="$muted500">
              Gluetun is not reachable. It only starts once a valid provider
              config has been saved and the plugin restarted.
            </Text>
          ) : null}
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="VPN provider" />
        <VStack space="md">
          <PillGroup
            options={providers.map((p) => ({ value: p.Name, label: p.Label }))}
            value={form.Provider}
            onChange={(name) => {
              const next = providers.find((p) => p.Name === name)
              setForm((f) => ({
                ...f,
                Provider: name,
                VPNType:
                  next && !next.VPNTypes.includes(f.VPNType)
                    ? next.VPNTypes[0]
                    : f.VPNType
              }))
            }}
          />
          <PillGroup
            options={vpnTypes.map((t) => ({
              value: t,
              label: t === 'wireguard' ? 'WireGuard' : 'OpenVPN'
            }))}
            value={form.VPNType}
            onChange={setField('VPNType')}
          />

          {isWireguard ? (
            <VStack space="md">
              <TextField
                label="WireGuard private key"
                value={form.WireguardPrivateKey}
                onChangeText={setField('WireguardPrivateKey')}
                placeholder={
                  config?.WireguardKeySet ? '(unchanged)' : 'base64 private key'
                }
                helper="Write-only: never shown again after saving"
                secureTextEntry
              />
              <TextField
                label="WireGuard preshared key (optional)"
                value={form.WireguardPresharedKey}
                onChangeText={setField('WireguardPresharedKey')}
                placeholder={
                  config?.WireguardPresharedKeySet
                    ? '(unchanged)'
                    : 'base64 preshared key'
                }
                secureTextEntry
              />
              <TextField
                label="WireGuard addresses"
                value={form.WireguardAddresses}
                onChangeText={setField('WireguardAddresses')}
                placeholder="10.64.222.21/32"
                helper="Comma separated CIDRs assigned by your provider"
              />
              {isCustom ? (
                <VStack space="md">
                  <TextField
                    label="Server public key"
                    value={form.WireguardPublicKey}
                    onChangeText={setField('WireguardPublicKey')}
                    placeholder="base64 public key"
                  />
                  <TextField
                    label="Server endpoint IP"
                    value={form.WireguardEndpointIP}
                    onChangeText={setField('WireguardEndpointIP')}
                    placeholder="203.0.113.10"
                  />
                  <TextField
                    label="Server endpoint port"
                    value={form.WireguardEndpointPort}
                    onChangeText={setField('WireguardEndpointPort')}
                    placeholder="51820"
                  />
                </VStack>
              ) : null}
            </VStack>
          ) : (
            <VStack space="md">
              <TextField
                label="OpenVPN username"
                value={form.OpenVPNUser}
                onChangeText={setField('OpenVPNUser')}
                placeholder="username"
              />
              <TextField
                label="OpenVPN password"
                value={form.OpenVPNPassword}
                onChangeText={setField('OpenVPNPassword')}
                placeholder={
                  config?.OpenVPNPasswordSet ? '(unchanged)' : 'password'
                }
                helper="Write-only: never shown again after saving"
                secureTextEntry
              />
            </VStack>
          )}
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="Server filters & network" />
        <VStack space="md">
          <TextField
            label="Server countries"
            value={form.ServerCountries}
            onChangeText={setField('ServerCountries')}
            placeholder="Netherlands, Sweden"
            helper="Comma separated, optional"
          />
          <TextField
            label="Server cities"
            value={form.ServerCities}
            onChangeText={setField('ServerCities')}
            placeholder="Amsterdam"
            helper="Comma separated, optional"
          />
          <TextField
            label="DNS address (optional)"
            value={form.DNSAddress}
            onChangeText={setField('DNSAddress')}
            placeholder="1.1.1.1"
            helper="Custom upstream for gluetun's DNS resolver"
          />
          <HStack justifyContent="space-between" alignItems="center">
            <Text size="sm">DNS over TLS</Text>
            <Toggle
              value={form.DNSOverTLS}
              onPress={() => setField('DNSOverTLS')(!form.DNSOverTLS)}
            />
          </HStack>
          <TextField
            label="Firewall outbound subnets"
            value={form.FirewallOutboundSubnets}
            onChangeText={setField('FirewallOutboundSubnets')}
            placeholder="192.168.2.0/24"
            helper="LAN subnets allowed outside the killswitch (needed for gateway replies to SPR devices)"
          />
          <Text size="xs" color="$muted500">
            The killswitch (FIREWALL=on) and DNS over TLS are always enforced;
            HTTP proxy and Shadowsocks are always off. Provider or credential
            changes take effect after restarting the plugin.
          </Text>
        </VStack>
      </Card>

      <ModalConfirm
        isOpen={showStop}
        onClose={() => setShowStop(false)}
        onConfirm={() => {
          setShowStop(false)
          setTunnel(false)
        }}
        title="Stop VPN tunnel?"
        message="Devices routed through the vpn-egress group will lose connectivity until the tunnel is started again (killswitch stays active)."
        confirmText="Stop tunnel"
        destructive
      />
    </Page>
  )
}
