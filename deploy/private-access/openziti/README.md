# Control One OpenZiti Out-of-Box Bundle

This bundle gives bank operators a repeatable path to operate OpenZiti beside
Control One for dark app, database, and admin services. OpenZiti remains the
zero-trust overlay and service-policy plane. Control One imports identities,
services, service policies, edge-router health, and audit events so exposure
reconciliation can prove a service is unreachable publicly but reachable through
approved OpenZiti policy.

The bundle intentionally stages the official OpenZiti installer and documents
the production service templates instead of vendoring environment-specific PKI
or controller configuration. Operators should review the generated controller,
router, tunneler, DNS, PKI, and ZAC settings inside the bank change window.

## Files

- `.env.example`: operator variables for controller URL, Control One URLs, and
  import credentials.
- `install-openziti-oob.sh`: stages the official OpenZiti installer, optionally
  runs it, writes the Control One provider-account payload, and can POST that
  payload to Control One.
- `control-one-provider-account.example.json`: a ready-to-edit provider account
  request for `/api/v1/private-access/provider-accounts`.
- `provider-manifest.json`: signed-offline-bundle compatible provider manifest
  for the Control One `private_access_provider_manifest` content type.
- `service-templates.json`: bank baseline intent for SSH, RDP, admin UI,
  database, and application services.

## Prerequisites

- Linux hosts sized for OpenZiti controller and edge routers.
- DNS name and TLS material for the controller and ZAC endpoint.
- Edge routers placed in the private networks that need dark services.
- OpenZiti CLI access for bootstrap administration.
- Control One admin token with access to the tenant receiving imports.
- A Control One provider credential for `provider=openziti` containing the
  OpenZiti management API token.

Official references:

- OpenZiti self-hosting and quickstarts: `https://openziti.io/docs/`
- OpenZiti controller and edge router concepts:
  `https://openziti.io/docs/learn/core-concepts/`
- OpenZiti tunnelers: `https://openziti.io/docs/reference/tunnelers/`
- ZAC administration console: `https://github.com/openziti/ziti-console`

## Control One Credential

Create the encrypted provider credential before enabling live imports:

```bash
curl -fsS \
  -H "Authorization: Bearer ${CONTROL_ONE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "'"${CONTROL_ONE_TENANT_ID}"'",
    "provider": "openziti",
    "name": "openziti-prod",
    "config": {
      "base_url": "https://openziti.example.bank",
      "token": "replace-with-openziti-management-token"
    }
  }' \
  "${CONTROL_ONE_URL}/api/v1/provider-credentials"
```

Use the returned credential `id` as `CONTROL_ONE_CREDENTIAL_ID` in `.env`.

## Install Flow

1. Copy `.env.example` to `.env` and fill the required values.
2. Stage the official OpenZiti installer for review:

   ```bash
   FETCH_OPENZITI_INSTALLER=true ./install-openziti-oob.sh .env
   ```

3. Review controller PKI, edge-router placement, ZAC exposure, and tunneler
   enrollment procedures with the change owner.
4. Run the official installer when approved:

   ```bash
   RUN_OPENZITI_INSTALLER=true ./install-openziti-oob.sh .env
   ```

5. Generate the Control One provider-account payload:

   ```bash
   ./install-openziti-oob.sh .env
   ```

6. Apply the provider account automatically or POST
   `control-one-provider-account.json` yourself:

   ```bash
   APPLY_CONTROL_ONE_PROVIDER=true ./install-openziti-oob.sh .env
   ```

Once the provider account exists, Control One scheduled imports can pull
OpenZiti identities, services, service policies, edge-router health, and audit
events from the configured management endpoint.

## Production Baseline

- Publish SSH/RDP/admin/database/application endpoints as explicit OpenZiti
  services; do not leave the same listeners publicly exposed.
- Prefer role attributes such as `#secops-admins`, `#db-admins`,
  `#app-operators`, and `#break-glass` over individual long-lived identities.
- Separate bind identities for tunnelers/routers from dial identities for
  humans and workloads.
- Keep ZAC/admin UI behind OpenZiti or a hardened management network.
- Require ticket-backed expiry for break-glass identities and policies.
- Import OpenZiti into Control One at least every 15 minutes in production.

`service-templates.json` captures the baseline service-policy intent, while
`provider-manifest.json` wraps the same baseline in the signed offline-content
manifest shape. Banks should translate the intent into approved OpenZiti
services and policies, then let Control One imports verify the effective state.

## HA and Recovery Notes

- Run controller and edge-router infrastructure in separate failure domains
  where the bank architecture allows it.
- Back up controller database/Persistence, PKI, enrollment tokens, ZAC
  configuration, and router identities.
- Keep router enrollment JWTs short-lived and store them only in the approved
  secret manager.
- Monitor controller API health, router control-channel health, service-policy
  drift, ZAC certificate expiry, and Control One import-run failures.
- Document the restore order: DNS/TLS, controller state and PKI, ZAC, edge
  routers, tunnelers, then Control One imports.
