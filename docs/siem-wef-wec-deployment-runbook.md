# SIEM WEF/WEC Deployment Runbook

Status: first production runbook for bank Windows Event Forwarding into
Control One through an OTel-compatible edge collector.

## Scope

Use this runbook when a bank wants domain controllers, servers, or endpoints to
forward Windows Security, Sysmon, and PowerShell events to a Windows Event
Collector (WEC), then have Control One collect the WEC `ForwardedEvents` log
through a content-pack source with collector mode `wef`.

The preferred production pattern is source-initiated WEF:

- The WEC host owns the subscription definitions.
- Windows sources learn the WEC endpoint from Group Policy.
- The OTel-compatible edge collector reads `ForwardedEvents` locally on the WEC
  host with the `windows_event_log` receiver.
- Control One source health proves collection, parser status, and source
  attribution from the same edge collector path as other SIEM sources.

## Prerequisites

- Dedicated Windows Server WEC host close to the forwarding domain controllers
  or server fleet.
- Domain group that scopes the first rollout wave, for example
  `ControlOne-WEF-Forwarders-Canary`.
- Change approval for each channel and event ID set. Security, Sysmon, and
  PowerShell logs are high-sensitivity sources.
- WinRM allowed from forwarding sources to the WEC host on TCP 5985 for HTTP or
  TCP 5986 for HTTPS.
- For HTTPS subscriptions, a server-authentication certificate on the WEC host
  and the issuing CA thumbprint for Group Policy.
- Control One edge collector registered for the WEC host and approved to run a
  rendered content-pack candidate.

## Prepare The WEC Host

Run from an elevated PowerShell prompt on the WEC host:

```powershell
winrm quickconfig -q
wecutil qc /q
wevtutil sl ForwardedEvents /ms:1073741824
```

For high-volume domain controller environments, raise `ForwardedEvents` size
above 1 GiB after capacity testing. Keep WEC hosts dedicated; do not co-locate
WEC with unrelated workloads that can starve event collection.

If collecting Security log events, ensure the Windows forwarding service can
read Security events on the source machines. Microsoft documents adding
`NETWORK SERVICE` to the Event Log Readers group for Security forwarding. Apply
that through a scoped GPO or an equivalent hardened local group policy baseline.

## Configure Source-Initiated GPO

Create or edit a GPO linked only to the canary source OU or security group.

Policy path:

`Computer Configuration > Policies > Administrative Templates > Windows Components > Event Forwarding > Configure target Subscription Manager`

For HTTP in a controlled domain network:

```text
Server=http://wec01.bank.local:5985/wsman/SubscriptionManager/WEC,Refresh=60
```

For HTTPS:

```text
Server=HTTPS://wec01.bank.local:5986/wsman/SubscriptionManager/WEC,Refresh=60,IssuerCA=<ISSUER_CA_THUMBPRINT>
```

Also set WinRM to automatic startup for scoped sources:

```powershell
Set-Service WinRM -StartupType Automatic
Start-Service WinRM
```

## Create The WEC Subscription

Use one subscription per rollout class and sensitivity tier. Keep domain
controller Security events separate from workstation Sysmon and PowerShell
events so volume, alerting, and rollback can be managed independently.

Example source-initiated subscription XML:

```xml
<Subscription xmlns="http://schemas.microsoft.com/2006/03/windows/events/subscription">
  <SubscriptionId>controlone-dc-security-canary</SubscriptionId>
  <SubscriptionType>SourceInitiated</SubscriptionType>
  <Description>Control One canary domain-controller Security events</Description>
  <Enabled>true</Enabled>
  <Uri>http://schemas.microsoft.com/wbem/wsman/1/windows/EventLog</Uri>
  <ConfigurationMode>Custom</ConfigurationMode>
  <Delivery Mode="Push">
    <Batching>
      <MaxItems>50</MaxItems>
      <MaxLatencyTime>30000</MaxLatencyTime>
    </Batching>
    <PushSettings>
      <Heartbeat Interval="60000" />
    </PushSettings>
  </Delivery>
  <Query>
    <![CDATA[
      <QueryList>
        <Query Id="0" Path="Security">
          <Select Path="Security">
            *[System[(EventID=4624 or EventID=4625 or EventID=4634 or EventID=4647 or EventID=4688 or EventID=4689)]]
          </Select>
        </Query>
      </QueryList>
    ]]>
  </Query>
  <ReadExistingEvents>false</ReadExistingEvents>
  <TransportName>HTTP</TransportName>
  <ContentFormat>Events</ContentFormat>
  <LogFile>ForwardedEvents</LogFile>
  <AllowedSourceNonDomainComputers></AllowedSourceNonDomainComputers>
  <AllowedSourceDomainComputers>O:NSG:NSD:(A;;GA;;;DC)(A;;GA;;;NS)</AllowedSourceDomainComputers>
</Subscription>
```

Create and inspect the subscription:

```powershell
wecutil cs .\controlone-dc-security-canary.xml
wecutil es
wecutil gs controlone-dc-security-canary
```

After canary validation, add separate subscriptions for:

- `Microsoft-Windows-Sysmon/Operational` Event IDs 1 and 3 first, then expand.
- `Microsoft-Windows-PowerShell/Operational` script block/module logging events
  after legal/privacy approval.
- Domain controller account-management and directory-service events required by
  the bank detection catalog.

## Connect Control One

1. In Control One, approve or create the content-pack source for the WEC host
   with collector mode `wef`.
2. Render an OTel config candidate for the WEC edge collector.
3. Confirm the rendered YAML contains a receiver shaped like:

```yaml
receivers:
  windows_event_log/controlone.windows.forwarded:
    channel: ForwardedEvents
```

4. Approve the exact rendered config version and queue it to the WEC edge
   collector.
5. Confirm the edge collector reports the candidate as deployed and the source
   health row moves from `config_rendered` or `deployed` to `collecting`, then
   `parser_healthy` once parser fixtures pass.

## Validate Event Flow

On a source computer:

```powershell
wevtutil qe Microsoft-Windows-Eventlog-ForwardingPlugin/Operational /c:20 /f:text
```

Look for successful subscription registration and event delivery. Microsoft
documents event 104 as a normal success signal for source-initiated forwarding.

On the WEC host:

```powershell
wecutil gr controlone-dc-security-canary
Get-WinEvent -LogName ForwardedEvents -MaxEvents 20 |
  Select-Object TimeCreated, ProviderName, Id, MachineName
```

In Control One:

- Source health shows the WEC content-pack source as `collecting` or
  `parser_healthy`.
- Recent events include `event.module=windows`, `event.provider`,
  `event.code`, `host.hostname`, and source user/network/process aliases where
  the Windows event carries those fields.
- Parser-failed, silent, backpressured, or stale source-health rows generate a
  SOC investigation handoff with the source runtime evidence reference.

## Operate Safely

- Roll out by OU/security group waves, starting with a canary domain controller
  and a canary workstation/server group.
- Keep Security, Sysmon, and PowerShell in separate subscriptions until volume
  budgets are proven.
- Treat subscription XML and rendered OTel YAML as change-controlled artifacts.
- Monitor `ForwardedEvents` log utilization, WEC CPU, WinRM errors, and Control
  One edge collector queue pressure.
- Use HTTPS for cross-domain or less-trusted network segments. Prefer domain
  Kerberos/HTTP only on controlled internal paths with compensating network
  controls.
- Do not enable `ReadExistingEvents=true` for broad production groups without a
  backfill window and volume budget.

## Rollback

Disable a subscription without deleting it:

```powershell
wecutil ss controlone-dc-security-canary /e:false
```

Remove or narrow the target Subscription Manager GPO for the affected source
group, then run `gpupdate /force` during the maintenance window. In Control One,
queue the previous deployed edge collector config candidate or disable the WEC
source proposal. Preserve the WEC subscription XML, Group Policy backup, and
Control One rendered config version for audit.

## Troubleshooting

- No sources appear: verify the GPO target Subscription Manager string, DNS,
  WinRM service, firewall rules, and `wecutil gr <subscription>`.
- `Access is denied` for Security events: verify the Security log reader
  permission baseline, including `NETWORK SERVICE` in Event Log Readers where
  required.
- Events reach `ForwardedEvents` but not Control One: verify the deployed OTel
  receiver channel, edge collector process health, and Control One source-health
  receiver ID.
- Events are present but parser health fails: export one XML/JSON sample from
  `ForwardedEvents`, replay it through the content-pack sample harness, and add
  or quarantine the parser pack before widening rollout.
- WEC backlog grows: reduce subscription scope, separate high-volume Sysmon
  events, increase WEC capacity, or add another collector tier.

## References

- Microsoft source-initiated subscription setup:
  https://learn.microsoft.com/en-us/windows/win32/wec/setting-up-a-source-initiated-subscription
- Microsoft `wecutil` reference:
  https://learn.microsoft.com/en-us/windows/win32/wec/wecutil
- Microsoft Defender for Identity WEF example:
  https://learn.microsoft.com/en-us/defender-for-identity/deploy/configure-event-forwarding
