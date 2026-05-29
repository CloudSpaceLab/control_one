# Offline Content Pack Examples

These example manifests are payloads that can be referenced from a signed
offline bundle `manifest.json`.

Supported production content types now include:

- `vulnerability_feed`
- `siem_content_pack`
- `private_access_provider_manifest`
- `remediation_pack`
- `ai_investigation_pack`

For `remediation_pack`, high and critical risk actions must declare approval,
`min_approvers >= 2`, and `separation_of_duties=true`. Script bodies stay in
signed artifacts referenced by SHA-256; the shared action-plan diff should carry
operator intent, rollback, verification, and provenance rather than raw scripts.

For `ai_investigation_pack`, tools must require citations and declare guardrails
so offline AI workflows remain evidence-first.

## Build and Sign

If the bundle includes vulnerability content, first normalize upstream advisory
exports into the signed `vulnerability_feed` schema:

```bash
go run ./controlplane/cmd/vulnerability-feed-factory \
  --source bank-feed-2026-05-29 \
  --input "osv:./feeds/osv/*.json" \
  --input "github:./feeds/github-advisories.json" \
  --input "nvd:./feeds/nvdcve-2.0-2026.json" \
  --input "cisa-kev:./feeds/known_exploited_vulnerabilities.json" \
  --out ./bundle-root/content/vulnerability-feed.json
```

Use the factory command to compute content SHA-256 values, sign the final
`manifest.json`, write `manifest.sig`, create the `.tar.gz`, and self-verify
the result with the same verifier used by the control plane:

```bash
go run ./controlplane/cmd/offline-content-factory \
  --manifest ./manifest.json \
  --content-root ./bundle-root \
  --private-key ./offline-content.key \
  --out ./control-one-content-2026.05.29.tar.gz \
  --print-public-key
```

Install the printed public key into the control-plane
`offline_content.public_key_file` setting before importing the signed archive.
