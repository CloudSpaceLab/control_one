# Control One Headscale Out-of-Box Bundle

This bundle gives bank operators a repeatable path to support Headscale beside
Control One where teams already use the Tailscale client model but need a
bank-owned control server. Headscale remains the coordination server and ACL
source of truth. Control One imports nodes, users/namespaces, routes, and ACL
rules so the control room can reason about private reachability and route
approval evidence without depending on Tailscale SaaS.

The bundle does not vendor a generated Headscale configuration because banks
usually vary OIDC, DNS, TLS, database, and ACL-file management. It stages the
official install notes, generates the Control One provider-account payload, and
ships a baseline ACL/route-approval intent for review.

## Files

- `.env.example`: operator variables for Headscale URL, Control One URLs, ACL
  file path, and import credentials.
- `install-headscale-oob.sh`: stages official install notes, optionally runs a
  locally supplied install command, writes the Control One provider-account
  payload, and can POST that payload to Control One.
- `control-one-provider-account.example.json`: a ready-to-edit provider account
  request for `/api/v1/private-access/provider-accounts`.
- `provider-manifest.json`: signed-offline-bundle compatible provider manifest
  for the Control One `private_access_provider_manifest` content type.
- `acl-templates.json`: bank baseline intent for OIDC groups, preauth keys,
  ACL rules, route approval, and break-glass.

## Prerequisites

- Linux host or HA pair sized for Headscale.
- PostgreSQL or another approved persistent database backend.
- TLS and DNS for `HEADSCALE_URL`.
- OIDC client configured in the bank identity provider.
- Tailscale clients approved for enrolled platforms.
- Control One admin token with access to the tenant receiving imports.
- A Control One provider credential for `provider=headscale` containing the
  Headscale API key.

Official references:

- Headscale documentation: `https://headscale.net/`
- Headscale configuration: `https://headscale.net/stable/ref/configuration/`
- Headscale ACL policy: `https://headscale.net/stable/ref/acls/`
- Headscale routes: `https://headscale.net/stable/ref/routes/`

## Control One Credential

Create the encrypted provider credential before enabling live imports:

```bash
curl -fsS \
  -H "Authorization: Bearer ${CONTROL_ONE_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{
    "tenant_id": "'"${CONTROL_ONE_TENANT_ID}"'",
    "provider": "headscale",
    "name": "headscale-prod",
    "config": {
      "base_url": "https://headscale.example.bank",
      "token": "replace-with-headscale-api-key"
    }
  }' \
  "${CONTROL_ONE_URL}/api/v1/provider-credentials"
```

Use the returned credential `id` as `CONTROL_ONE_CREDENTIAL_ID` in `.env`.

## Install Flow

1. Copy `.env.example` to `.env` and fill the required values.
2. Stage the official Headscale install notes for review:

   ```bash
   FETCH_HEADSCALE_INSTALLER=true ./install-headscale-oob.sh .env
   ```

3. Review OIDC, TLS, database backup, ACL file ownership, preauth-key process,
   and route-approval ownership with the bank change owner.
4. Apply the reviewed Headscale deployment using the bank-approved method. The
   script will not run package-manager commands unless `HEADSCALE_INSTALL_CMD`
   is explicitly supplied and `RUN_HEADSCALE_INSTALLER=true`.
5. Generate the Control One provider-account payload:

   ```bash
   ./install-headscale-oob.sh .env
   ```

6. Apply the provider account automatically or POST
   `control-one-provider-account.json` yourself:

   ```bash
   APPLY_CONTROL_ONE_PROVIDER=true ./install-headscale-oob.sh .env
   ```

Once the provider account exists, Control One scheduled imports can pull
Headscale nodes, routes, users/namespaces, and ACL rules from the configured
management endpoint.

## Production Baseline

- Use OIDC groups for durable access intent. Avoid long-lived manual user
  exceptions outside the ACL file.
- Use preauth keys only for approved automation, keep them short-lived, and
  avoid reusable production keys unless formally approved.
- Explicitly approve advertised subnet routes before using them for production
  access.
- Keep route owners separate from broad human admin groups.
- Use `tag:break-glass` only with ticket-backed, time-boxed membership.
- Import Headscale into Control One at least every 15 minutes in production.

`acl-templates.json` captures these rules as implementation intent, while
`provider-manifest.json` wraps the same baseline in the signed offline-content
manifest shape. Banks should translate the intent into their approved ACL
policy and route approval process, then let Control One imports verify the
effective state.

## HA and Recovery Notes

- Put database, TLS, DNS, reverse proxy, and Headscale service recovery into
  the bank backup plan.
- Back up the Headscale database, config file, ACL file, OIDC client metadata,
  and API-key creation/rotation evidence.
- Monitor node check-in age, advertised but unapproved routes, ACL drift,
  certificate expiry, and Control One import-run failures.
- Document the restore order: database, config/TLS, Headscale service, OIDC,
  route approvals, then Control One imports.
