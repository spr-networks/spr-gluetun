import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  api,
  useAlert,
  Page,
  ListHeader,
  Card,
  SectionHeader,
  StatTile,
  StatusDot,
  Toggle,
  TextField,
  ModalConfirm,
  Loading,
  Badge,
  BadgeText,
  Box,
  Button,
  ButtonText,
  Heading,
  HStack,
  VStack,
  Text
} from '@spr-networks/plugin-ui'

const PLUGIN_BASE = `/plugins/${api.pluginURI() || 'spr-gluetun'}`

// fallback only; the backend reports the live value in GET /status
const DEFAULT_GATEWAY_IP = '172.30.117.2'

const splitList = (value) =>
  (value || '')
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item.length)

const joinList = (items) => (items && items.length ? items.join(', ') : '')

const isWireguardKey = (value) => {
  try {
    return atob(value).length === 32
  } catch (e) {
    return false
  }
}

const isIPAddress = (value) =>
  /^\d{1,3}(\.\d{1,3}){3}$/.test(value) ||
  (/^[0-9a-fA-F:]+$/.test(value) && value.includes(':'))

const cidrListError = (value) => {
  const bad = splitList(value).find((entry) => !entry.includes('/'))
  return bad
    ? `"${bad}" is missing a prefix length (e.g. ${bad}/32)`
    : null
}

// what the form looks like for a given saved config; secrets always start
// empty because they are write-only server-side
const formFromConfig = (cfg) => ({
  Provider: cfg.Provider || '',
  VPNType: cfg.VPNType || 'wireguard',
  WireguardPrivateKey: '',
  WireguardPresharedKey: '',
  WireguardAddresses: joinList(cfg.WireguardAddresses),
  WireguardPublicKey: cfg.WireguardPublicKey || '',
  WireguardEndpointIP: cfg.WireguardEndpointIP || '',
  WireguardEndpointPort: cfg.WireguardEndpointPort
    ? String(cfg.WireguardEndpointPort)
    : '',
  OpenVPNUser: cfg.OpenVPNUser || '',
  OpenVPNPassword: '',
  ServerCountries: joinList(cfg.ServerCountries),
  ServerCities: joinList(cfg.ServerCities),
  DNSAddress: cfg.DNSAddress || '',
  DNSOverTLS: !cfg.DisableDNSOverTLS,
  FirewallOutboundSubnets: joinList(cfg.FirewallOutboundSubnets)
})

const MonoText = ({ children, ...props }) => (
  <Text
    color="$textLight900"
    sx={{ _dark: { color: '$textDark50' }, '@base': { fontFamily: 'monospace' } }}
    {...props}
  >
    {children}
  </Text>
)

const GroupLabel = ({ children }) => (
  <Text
    size="2xs"
    color="$muted500"
    fontWeight="$medium"
    sx={{ '@base': { letterSpacing: 0.6, textTransform: 'uppercase' } }}
  >
    {children}
  </Text>
)

const ChipGroup = ({ label, options, value, onChange }) => (
  <VStack space="xs">
    <GroupLabel>{label}</GroupLabel>
    <HStack flexWrap="wrap" gap="$2">
      {options.map((option) => (
        <Button
          key={option.value}
          size="xs"
          borderRadius="$full"
          variant={option.value === value ? 'solid' : 'outline'}
          onPress={() => onChange(option.value)}
        >
          <ButtonText>{option.label}</ButtonText>
        </Button>
      ))}
    </HStack>
  </VStack>
)

const Segmented = ({ options, value, onChange }) => (
  <HStack gap="$2" flexWrap="wrap">
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

const Step = ({ n, title, text }) => (
  <HStack space="md" alignItems="flex-start">
    <Box
      w={24}
      h={24}
      borderRadius="$full"
      bg="$primary700"
      alignItems="center"
      justifyContent="center"
      flexShrink={0}
      sx={{ _dark: { bg: '$primary500' } }}
    >
      <Text size="xs" color="$white" fontWeight="$semibold">
        {n}
      </Text>
    </Box>
    <VStack space="xs" flexShrink={1}>
      <Text
        size="sm"
        fontWeight="$semibold"
        color="$textLight900"
        sx={{ _dark: { color: '$textDark50' } }}
      >
        {title}
      </Text>
      <Text size="xs" color="$muted500" lineHeight="$sm">
        {text}
      </Text>
    </VStack>
  </HStack>
)

// write-only secret: once configured, show "Configured ✓" + an explicit
// Replace affordance instead of an empty field pretending to be the value
const SecretField = ({
  label,
  configured,
  replacing,
  onReplace,
  onCancelReplace,
  value,
  onChangeText,
  placeholder,
  helper,
  error
}) => {
  if (configured && !replacing) {
    return (
      <VStack space="xs">
        <Text
          size="sm"
          fontWeight="$semibold"
          color="$textLight800"
          sx={{ _dark: { color: '$textDark100' } }}
        >
          {label}
        </Text>
        <HStack space="md" alignItems="center">
          <Badge action="success" variant="outline" borderRadius="$full">
            <BadgeText>Configured ✓</BadgeText>
          </Badge>
          <Button size="xs" variant="outline" onPress={onReplace}>
            <ButtonText>Replace</ButtonText>
          </Button>
        </HStack>
      </VStack>
    )
  }

  return (
    <VStack space="xs">
      <TextField
        label={label}
        value={value}
        onChangeText={onChangeText}
        placeholder={placeholder}
        helper={helper}
        error={error}
        secureTextEntry
      />
      {configured ? (
        <Button size="xs" variant="link" alignSelf="flex-start" onPress={onCancelReplace}>
          <ButtonText>Keep existing value</ButtonText>
        </Button>
      ) : null}
    </VStack>
  )
}

export default function Plugin() {
  const alert = useAlert()
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [status, setStatus] = useState(null)
  const [providers, setProviders] = useState([])
  const [config, setConfig] = useState(null)
  const [saving, setSaving] = useState(false)
  const [tunnelBusy, setTunnelBusy] = useState(null) // 'start' | 'stop' | 'restart'
  const [showStop, setShowStop] = useState(false)
  const [replacing, setReplacing] = useState({})
  const [form, setForm] = useState(formFromConfig({}))
  const [baseline, setBaseline] = useState(JSON.stringify(formFromConfig({})))

  const statusTimer = useRef(null)

  const setField = (key) => (value) => setForm((f) => ({ ...f, [key]: value }))

  const setReplace = (key, on) => {
    setReplacing((r) => ({ ...r, [key]: on }))
    if (!on) {
      setField(key)('')
    }
  }

  const adoptConfig = (cfg) => {
    setConfig(cfg)
    const f = formFromConfig(cfg)
    setForm(f)
    setBaseline(JSON.stringify(f))
    setReplacing({})
  }

  const refreshStatus = useCallback(() => {
    api
      .get(`${PLUGIN_BASE}/status`)
      .then(setStatus)
      .catch(() => setStatus(null))
  }, [])

  const loadAll = useCallback(() => {
    setLoading(true)
    setLoadError(false)
    Promise.all([
      api.get(`${PLUGIN_BASE}/config`),
      api.get(`${PLUGIN_BASE}/providers`),
      api.get(`${PLUGIN_BASE}/status`).catch(() => null)
    ])
      .then(([cfg, provs, stat]) => {
        setProviders(provs || [])
        setStatus(stat)
        adoptConfig(cfg)
      })
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    loadAll()
    statusTimer.current = setInterval(refreshStatus, 10000)
    return () => clearInterval(statusTimer.current)
  }, [])

  const copyText = (label, value) => {
    navigator.clipboard
      .writeText(value)
      .then(() => alert.success(`${label} copied`))
      .catch(() => alert.error('Copy failed'))
  }

  // ---- derived state -------------------------------------------------------

  const running = status?.VPNStatus === 'running'
  const reachable = !!status?.ControlReachable
  const configured = !!config?.Configured
  const gatewayIP = status?.GatewayIP || DEFAULT_GATEWAY_IP

  const provider = providers.find((p) => p.Name === form.Provider)
  const vpnTypes = provider?.VPNTypes || ['wireguard', 'openvpn']
  const isWireguard = form.VPNType === 'wireguard'
  const isCustom = form.Provider === 'custom'

  const providerLabel = (name) =>
    providers.find((p) => p.Name === name)?.Label || name || '—'

  const errors = useMemo(() => {
    const e = {}
    if (isWireguard) {
      if (form.WireguardPrivateKey && !isWireguardKey(form.WireguardPrivateKey)) {
        e.WireguardPrivateKey = 'Must be base64 and decode to 32 bytes'
      }
      if (form.WireguardPresharedKey && !isWireguardKey(form.WireguardPresharedKey)) {
        e.WireguardPresharedKey = 'Must be base64 and decode to 32 bytes'
      }
      const addrErr = cidrListError(form.WireguardAddresses)
      if (addrErr) e.WireguardAddresses = addrErr
      if (isCustom) {
        if (form.WireguardPublicKey && !isWireguardKey(form.WireguardPublicKey)) {
          e.WireguardPublicKey = 'Must be base64 and decode to 32 bytes'
        }
        if (form.WireguardEndpointIP && !isIPAddress(form.WireguardEndpointIP)) {
          e.WireguardEndpointIP = 'Enter an IP address, not a hostname'
        }
        if (form.WireguardEndpointPort) {
          const port = parseInt(form.WireguardEndpointPort, 10)
          if (isNaN(port) || port < 1 || port > 65535) {
            e.WireguardEndpointPort = 'Port must be 1–65535'
          }
        }
      }
    }
    if (form.DNSAddress && !isIPAddress(form.DNSAddress)) {
      e.DNSAddress = 'Enter an IP address (e.g. 1.1.1.1)'
    }
    const fwErr = cidrListError(form.FirewallOutboundSubnets)
    if (fwErr) e.FirewallOutboundSubnets = fwErr
    return e
  }, [form, isWireguard, isCustom])

  const missingRequired = useMemo(() => {
    if (!form.Provider) return true
    if (isWireguard) {
      if (!config?.WireguardKeySet && !form.WireguardPrivateKey) return true
      if (!splitList(form.WireguardAddresses).length) return true
      if (isCustom) {
        if (!form.WireguardPublicKey || !form.WireguardEndpointIP) return true
        if (!form.WireguardEndpointPort) return true
      }
    } else {
      if (!form.OpenVPNUser) return true
      if (!config?.OpenVPNPasswordSet && !form.OpenVPNPassword) return true
    }
    return false
  }, [form, config, isWireguard, isCustom])

  const dirty = JSON.stringify(form) !== baseline
  const canSave = dirty && !missingRequired && Object.keys(errors).length === 0

  // ---- actions --------------------------------------------------------------

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

    setSaving(true)
    api
      .put(`${PLUGIN_BASE}/config`, payload)
      .then((cfg) => {
        adoptConfig(cfg)
        refreshStatus()
        alert.success(
          'Saved. Restart the plugin (Plugins page) to apply provider or credential changes.'
        )
      })
      .catch((err) => alert.error('Failed to save configuration', err))
      .finally(() => setSaving(false))
  }

  const setTunnel = (start) => {
    setTunnelBusy(start ? 'start' : 'stop')
    api
      .put(`${PLUGIN_BASE}/vpn`, { Status: start ? 'running' : 'stopped' })
      .then(() => {
        alert.success(start ? 'Tunnel starting' : 'Tunnel stopped')
        setTimeout(refreshStatus, 1500)
      })
      .catch((err) => alert.error('Tunnel command failed', err))
      .finally(() => setTunnelBusy(null))
  }

  const restartTunnel = () => {
    setTunnelBusy('restart')
    api
      .post(`${PLUGIN_BASE}/restart`)
      .then(() => {
        alert.success('Tunnel restarted')
        setTimeout(refreshStatus, 1500)
      })
      .catch((err) => alert.error('Restart failed', err))
      .finally(() => setTunnelBusy(null))
  }

  // ---- header ----------------------------------------------------------------

  const headerStatus = !configured
    ? 'Not configured'
    : running
    ? 'Connected'
    : reachable
    ? 'Stopped'
    : 'Unreachable'
  const headerAction = !configured
    ? 'muted'
    : running
    ? 'success'
    : reachable
    ? 'warning'
    : 'error'

  const header = (
    <ListHeader
      title="Gluetun VPN gateway"
      description="Route devices in the vpn-egress group through an outbound VPN tunnel"
      mark="gt"
      status={loading || loadError ? undefined : headerStatus}
      statusAction={headerAction}
    >
      {!loading && !loadError ? (
        <Button size="sm" isDisabled={!canSave || saving} onPress={save}>
          <ButtonText>{saving ? 'Saving…' : 'Save changes'}</ButtonText>
        </Button>
      ) : null}
    </ListHeader>
  )

  if (loading) {
    return (
      <Page>
        {header}
        <Loading />
      </Page>
    )
  }

  if (loadError) {
    return (
      <Page>
        {header}
        <Card>
          <VStack space="md" alignItems="flex-start">
            <HStack space="sm" alignItems="center">
              <StatusDot online={false} />
              <Heading size="sm" color="$textLight900" sx={{ _dark: { color: '$textDark50' } }}>
                Plugin backend unreachable
              </Heading>
            </HStack>
            <Text size="sm" color="$muted500" lineHeight="$sm">
              The spr-gluetun service did not respond. Make sure the plugin is
              enabled on the Plugins page, then retry.
            </Text>
            <Button size="sm" onPress={loadAll}>
              <ButtonText>Retry</ButtonText>
            </Button>
          </VStack>
        </Card>
      </Page>
    )
  }

  // ---- hero content -----------------------------------------------------------

  const exitLocation = [status?.City, status?.Country].filter(Boolean).join(', ')
  const stateWord = running ? 'Connected' : reachable ? 'Stopped' : 'Gateway unreachable'
  const stateHint = reachable
    ? 'Devices in vpn-egress have no internet until the tunnel is started (killswitch).'
    : 'Gluetun starts after a valid provider config is saved and the plugin is restarted.'

  const savedFilter =
    joinList(config?.ServerCities) || joinList(config?.ServerCountries) || 'Any location'
  const savedDNS = config?.DNSAddress
    ? config.DNSAddress
    : config?.DisableDNSOverTLS
    ? 'Plain DNS'
    : 'DoT (default)'

  const wgProviders = providers.filter(
    (p) => p.Name !== 'custom' && p.VPNTypes.includes('wireguard')
  )
  const ovpnOnlyProviders = providers.filter(
    (p) => p.Name !== 'custom' && !p.VPNTypes.includes('wireguard')
  )
  const customProvider = providers.filter((p) => p.Name === 'custom')

  const pickProvider = (name) => {
    const next = providers.find((p) => p.Name === name)
    setForm((f) => ({
      ...f,
      Provider: name,
      VPNType:
        next && !next.VPNTypes.includes(f.VPNType) ? next.VPNTypes[0] : f.VPNType
    }))
  }

  return (
    <Page>
      {header}

      {!configured ? (
        <Card>
          <SectionHeader title="Set up your VPN gateway" />
          <VStack space="lg">
            <Step
              n={1}
              title="Choose a provider"
              text="Pick your VPN service below, or Custom to use your own WireGuard server."
            />
            <Step
              n={2}
              title="Paste credentials"
              text="The WireGuard key and addresses (or OpenVPN login) from your provider's dashboard."
            />
            <Step
              n={3}
              title="Save"
              text="Settings are validated and stored on the router. Secrets are write-only and never shown again."
            />
            <Step
              n={4}
              title="Start the tunnel"
              text="Restart the plugin once (Plugins page) so gluetun picks up the config, then start the tunnel here."
            />
          </VStack>
        </Card>
      ) : (
        <Card>
          <HStack space="md" alignItems="center" mb="$4">
            <StatusDot online={running} warn={reachable && !running} size={12} />
            <VStack space="xs" flexShrink={1}>
              <Heading size="md" color="$textLight900" sx={{ _dark: { color: '$textDark50' } }}>
                {stateWord}
              </Heading>
              {running && status?.PublicIP ? (
                <HStack space="sm" alignItems="center" flexWrap="wrap">
                  <MonoText size="sm" fontWeight="$semibold">
                    {status.PublicIP}
                  </MonoText>
                  {exitLocation ? (
                    <Text size="sm" color="$muted500">
                      · {exitLocation}
                    </Text>
                  ) : null}
                  <Button
                    size="xs"
                    variant="link"
                    onPress={() => copyText('Public IP', status.PublicIP)}
                  >
                    <ButtonText>Copy</ButtonText>
                  </Button>
                </HStack>
              ) : (
                <Text size="sm" color="$muted500" lineHeight="$sm">
                  {stateHint}
                </Text>
              )}
            </VStack>
          </HStack>

          <HStack flexWrap="wrap" gap="$2">
            <StatTile label="Provider" value={providerLabel(config?.Provider)} />
            <StatTile
              label="Tunnel type"
              value={config?.VPNType === 'openvpn' ? 'OpenVPN' : 'WireGuard'}
            />
            <StatTile label="Server filter" value={savedFilter} />
            <StatTile label="DNS" value={savedDNS} mono={!!config?.DNSAddress} />
          </HStack>

          <Box
            mt="$4"
            p="$3.5"
            borderRadius="$xl"
            borderWidth={1}
            borderColor="$muted100"
            bg="$backgroundContentLight"
            sx={{
              _dark: { bg: '$backgroundContentDark', borderColor: '$borderColorCardDark' }
            }}
          >
            <HStack justifyContent="space-between" alignItems="center" flexWrap="wrap" gap="$3">
              <VStack space="xs" flexShrink={1}>
                <GroupLabel>Gateway IP</GroupLabel>
                <MonoText size="md" fontWeight="$semibold">
                  {gatewayIP}
                </MonoText>
                <Text size="xs" color="$muted500" lineHeight="$sm">
                  Point vpn-egress devices here. If the tunnel drops, traffic is
                  blocked by the killswitch — never leaked to the WAN.
                </Text>
              </VStack>
              <Button
                size="xs"
                variant="outline"
                onPress={() => copyText('Gateway IP', gatewayIP)}
              >
                <ButtonText>Copy</ButtonText>
              </Button>
            </HStack>
          </Box>

          <HStack mt="$4" gap="$2" flexWrap="wrap" alignItems="center">
            <Button
              size="sm"
              isDisabled={!reachable || running || tunnelBusy !== null}
              onPress={() => setTunnel(true)}
            >
              <ButtonText>{tunnelBusy === 'start' ? 'Starting…' : 'Start'}</ButtonText>
            </Button>
            <Button
              size="sm"
              variant="outline"
              action="negative"
              isDisabled={!reachable || !running || tunnelBusy !== null}
              onPress={() => setShowStop(true)}
            >
              <ButtonText>{tunnelBusy === 'stop' ? 'Stopping…' : 'Stop'}</ButtonText>
            </Button>
            <Button
              size="sm"
              variant="outline"
              isDisabled={!reachable || !running || tunnelBusy !== null}
              onPress={restartTunnel}
            >
              <ButtonText>{tunnelBusy === 'restart' ? 'Restarting…' : 'Restart'}</ButtonText>
            </Button>
            <Text size="xs" color="$muted500">
              Acts on the tunnel inside the running container.
            </Text>
          </HStack>
        </Card>
      )}

      <Card>
        <SectionHeader title="VPN provider" />
        <VStack space="lg">
          <Box sx={{ '@base': { maxHeight: 240, overflowY: 'auto' } }} pr="$1">
            <VStack space="md">
              <ChipGroup
                label="WireGuard capable"
                options={wgProviders.map((p) => ({ value: p.Name, label: p.Label }))}
                value={form.Provider}
                onChange={pickProvider}
              />
              <ChipGroup
                label="OpenVPN only"
                options={ovpnOnlyProviders.map((p) => ({ value: p.Name, label: p.Label }))}
                value={form.Provider}
                onChange={pickProvider}
              />
              <ChipGroup
                label="Bring your own server"
                options={customProvider.map((p) => ({ value: p.Name, label: 'Custom server' }))}
                value={form.Provider}
                onChange={pickProvider}
              />
            </VStack>
          </Box>

          <VStack space="xs">
            <Text
              size="sm"
              fontWeight="$semibold"
              color="$textLight800"
              sx={{ _dark: { color: '$textDark100' } }}
            >
              Tunnel type
            </Text>
            <Segmented
              options={vpnTypes.map((t) => ({
                value: t,
                label: t === 'wireguard' ? 'WireGuard' : 'OpenVPN'
              }))}
              value={form.VPNType}
              onChange={setField('VPNType')}
            />
            {form.Provider && vpnTypes.length === 1 ? (
              <Text size="xs" color="$muted500">
                {providerLabel(form.Provider)} supports{' '}
                {vpnTypes[0] === 'wireguard' ? 'WireGuard' : 'OpenVPN'} only.
              </Text>
            ) : null}
          </VStack>

          {isWireguard ? (
            <VStack space="md">
              <SecretField
                label="WireGuard private key"
                configured={!!config?.WireguardKeySet}
                replacing={!!replacing.WireguardPrivateKey}
                onReplace={() => setReplace('WireguardPrivateKey', true)}
                onCancelReplace={() => setReplace('WireguardPrivateKey', false)}
                value={form.WireguardPrivateKey}
                onChangeText={setField('WireguardPrivateKey')}
                placeholder="base64 private key"
                helper="Write-only: stored on the router, never shown again"
                error={errors.WireguardPrivateKey}
              />
              <SecretField
                label="WireGuard preshared key (optional)"
                configured={!!config?.WireguardPresharedKeySet}
                replacing={!!replacing.WireguardPresharedKey}
                onReplace={() => setReplace('WireguardPresharedKey', true)}
                onCancelReplace={() => setReplace('WireguardPresharedKey', false)}
                value={form.WireguardPresharedKey}
                onChangeText={setField('WireguardPresharedKey')}
                placeholder="base64 preshared key"
                error={errors.WireguardPresharedKey}
              />
              <TextField
                label="WireGuard addresses"
                value={form.WireguardAddresses}
                onChangeText={setField('WireguardAddresses')}
                placeholder="10.64.222.21/32"
                helper="Comma separated CIDRs assigned by your provider"
                error={errors.WireguardAddresses}
              />
              {isCustom ? (
                <VStack space="md">
                  <TextField
                    label="Server public key"
                    value={form.WireguardPublicKey}
                    onChangeText={setField('WireguardPublicKey')}
                    placeholder="base64 public key"
                    error={errors.WireguardPublicKey}
                  />
                  <TextField
                    label="Server endpoint IP"
                    value={form.WireguardEndpointIP}
                    onChangeText={setField('WireguardEndpointIP')}
                    placeholder="203.0.113.10"
                    error={errors.WireguardEndpointIP}
                  />
                  <TextField
                    label="Server endpoint port"
                    value={form.WireguardEndpointPort}
                    onChangeText={setField('WireguardEndpointPort')}
                    placeholder="51820"
                    error={errors.WireguardEndpointPort}
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
              <SecretField
                label="OpenVPN password"
                configured={!!config?.OpenVPNPasswordSet}
                replacing={!!replacing.OpenVPNPassword}
                onReplace={() => setReplace('OpenVPNPassword', true)}
                onCancelReplace={() => setReplace('OpenVPNPassword', false)}
                value={form.OpenVPNPassword}
                onChangeText={setField('OpenVPNPassword')}
                placeholder="password"
                helper="Write-only: stored on the router, never shown again"
              />
            </VStack>
          )}

          <Text size="xs" color="$muted500" lineHeight="$sm">
            Provider and credential changes take effect after restarting the
            plugin (Plugins page) — the tunnel restarts and devices briefly lose
            VPN egress.
          </Text>
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="Server filters & network" />
        <VStack space="md">
          <Text size="xs" color="$muted500" lineHeight="$sm">
            Filters narrow which of the provider's servers gluetun connects to.
            Leave empty to let the provider choose. Applied on plugin restart.
          </Text>
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
            error={errors.DNSAddress}
          />
          <HStack justifyContent="space-between" alignItems="center">
            <VStack space="xs" flexShrink={1}>
              <Text
                size="sm"
                fontWeight="$semibold"
                color="$textLight800"
                sx={{ _dark: { color: '$textDark100' } }}
              >
                DNS over TLS
              </Text>
              <Text size="xs" color="$muted500">
                Encrypt tunnel DNS lookups (recommended)
              </Text>
            </VStack>
            <Toggle
              label="DNS over TLS"
              value={form.DNSOverTLS}
              onPress={() => setField('DNSOverTLS')(!form.DNSOverTLS)}
            />
          </HStack>
          <TextField
            label="Firewall outbound subnets"
            value={form.FirewallOutboundSubnets}
            onChangeText={setField('FirewallOutboundSubnets')}
            placeholder="192.168.2.0/24"
            helper="Your SPR LAN subnets — required so gateway replies can reach devices through the killswitch"
            error={errors.FirewallOutboundSubnets}
          />
          <Text size="xs" color="$muted500" lineHeight="$sm">
            Always enforced: killswitch firewall on, HTTP proxy and Shadowsocks
            off, control server private to the plugin network.
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
        message="Devices in the vpn-egress group immediately lose internet access until the tunnel is started again — the killswitch blocks their traffic rather than leaking it to the WAN."
        confirmText="Stop tunnel"
        destructive
      />
    </Page>
  )
}
