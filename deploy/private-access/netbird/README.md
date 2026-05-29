# Control One NetBird Out-of-Box Bundle

This bundle gives bank operators a repeatable path to deploy self-hosted
NetBird beside Control One and connect it to the Control One private-access
import pipeline. NetBird remains the WireGuard overlay and policy plane;
Control One imports the inventory, routes, policy, and event evidence so the
control room can reason about private reachability without opening server
ports to the internet.

The bundle deliberately wraps the official NetBird self-hosting flow instead
of vendoring generated compose files. The current NetBird quickstart generates
deployment-specific files from `getting-started.sh`; operators should review
those files before first run and after each NetBird upgrade.

## Files

- `.env.example`: operator variables for DNS, Control One URLs, and import
  credentials.
- `install-netbird-oob.sh`: stages the official NetBird installer, optionally
  runs it, writes the Control One provider-account payload, and can POST that
  payload to Control One.
- `control-one-provider-account.example.json`: a ready-to-edit provider account
  request for `/api/v1/private-access/provider-accounts`.
- `provider-manifest.json`: signed-offline-bundle compatible provider manifest
  for the Control One `private_access_provider_manifest` content type.
- `policy-templates.json`: bank baseline policy intent for NetBird groups,
  routing peers, admin access, DB access, and break-glass.

## Prerequisites

- Linux VM or hosts sized for the NetBird control plane.
- Public DNS record for `NETBIRD_DOMAIN`.
- TCP 80 and 443 reachable for the NetBird control plane and TLS bootstrap.
- UDP 3478 reachable for STUN/TURN.
- Docker with compose plugin, `curl`, and `jq`.
- Control One admin token with access to the tenant receiving imports.
- A Control One provider credential for `provider=netbird` containing the
  NetBird API token.

Official references:

- NetBird self-hosting quickstart:
  `https://docs.netbird.io/selfhosted/selfhosted-quickstart`
- NetBird self-hosted configuration file reference:
  `https://docs.netbird.io/selfhosted/configuration-files`
- NetBird access control:
  `https://docs.netbird.io/manage/access-control/manage-network-access`
- NetBird routing peer behavior:
  `https://docs.netbird.io/manage/networks/how-routing-peers-work`

## Control One Credential

Create the encrypted provider credential before enabling live imports:

```bash
curl -fsS \
  -H "Authorization: Bearer ${CONTROL_ONE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "'"${CONTROL_ONE_TENANT_ID}"'",
    "provider": "netbird",
    "name": "netbird-prod",
    "config": {
      "base_url": "https://netbird.example.bank",
      "token": "replace-with-netbird-api-token"
    }
  }' \
  "${CONTROL_ONE_URL}/api/v1/provider-credentials"
```

Use the returned credential `id` as `CONTROL_ONE_CREDENTIAL_ID` in `.env`.

## Install Flow

1. Copy `.env.example` to `.env` and fill the required values.
2. Stage the official NetBird installer for review:

   ```bash
   FETCH_NETBIRD_INSTALLER=true ./install-netbird-oob.sh .env
   ```

3. Review `getting-started.sh`, generated configuration prompts, DNS, TLS, and
   firewall rules with the bank change window owner.
4. Run the official installer when approved:

   ```bash
   RUN_NETBIRD_INSTALLER=true ./install-netbird-oob.sh .env
   ```

5. Generate the Control One provider-account payload:

   ```bash
   ./install-netbird-oob.sh .env
   ```

6. Apply the provider account automatically or POST
   `control-one-provider-account.json` yourself:

   ```bash
   APPLY_CONTROL_ONE_PROVIDER=true ./install-netbird-oob.sh .env
   ```

Once the provider account exists, Control One scheduled imports will pull
NetBird peers, groups, policies, routes, and events from the configured
management endpoint.

## Production Baseline

- Disable NetBird's default all-to-all policy before go-live.
- Use explicit source and destination groups for every policy.
- Assign every routed subnet or network resource to an access-control group.
- Use routing peers for private subnets where agents cannot be installed.
- Register routing peers with setup keys and keep them out of human admin
  groups.
- Keep management-plane admin access separate from application and database
  access.
- Use break-glass groups with ticket references and time-boxed membership.
- Import NetBird into Control One at least every 15 minutes in production.

`policy-templates.json` captures these rules as implementation intent, while
`provider-manifest.json` wraps the same baseline in the signed offline-content
manifest shape. Neither file is a direct NetBird API export; banks should
translate the intent into their approved NetBird groups and policies, then let
Control One imports verify the effective state.

## HA and Recovery Notes

- Put DNS, reverse proxy, TLS, NetBird management, relay, and TURN/STUN
  responsibilities in separate failure domains where the bank's architecture
  allows it.
- Place relay/TURN near users and private environments; keep routing peers in
  the private networks they bridge.
- Back up generated NetBird configuration, compose files, IdP state, and any
  external database used by the deployment.
- Treat NetBird API tokens as bank secrets. Store them only in Control One
  encrypted provider credentials or the bank's approved secret manager.
- Monitor NetBird container health, certificate expiry, route peer connectivity,
  and Control One import-run failures.
- Document the restore order: DNS/TLS, IdP, NetBird control plane, relay/TURN,
  routing peers, then Control One imports.
