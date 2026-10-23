# Phase 0: recovery, trust and assessment — 23 October 2026

This is the current continuation on `0y3/Me/patch-posture-review`. It supersedes earlier statements that the signing fixes were only source tests and that no real finding had been resolved. **Phase 0 and Phase 1 remain open.** The prepared VirtualBox VM is still off while host RAM is constrained. No new `.ps1` file was created and no unrelated workload was stopped.

| Area | Verified progress | Requirement still open |
| --- | --- | --- |
| Recovery | Fresh PostgreSQL checkpoint restored in isolation; encrypted application recovery drill recorded below | Copy to a destination outside this machine, restore from that copy, and qualify the full agreed recovery scope/RPO/RTO |
| Trust | Qualified server and Linux agent deployed; independent pinned release check before installation; live signed policy/cache verified | Managed production HTTPS, trusted key distribution/ownership, enforced signed-only rollout, Windows independent bootstrap, replay/freshness and rotation |
| Assessment | Three selected Ubuntu advisories added alongside the two 7-Zip entries; one real affected package detected, upgraded through the app and its historical finding resolved | Comprehensive supported-product coverage, maintained upstream refresh, persisted per-node assessment completeness and full-host validation |
| Full Ubuntu host | Existing container remains enrolled; prepared VM preserved | RAM, guest access/enrollment, authentication logs, file monitoring, reboot and recovery tests |

## Actual assessment and remediation

Tenant **Local Development**, node **phase0-ubuntu-lab**, ID `d033fa85-5796-48ae-8e8a-f82706387326`. This Ubuntu 24.04 amd64 target is a container, not the prepared VM. Its unsupported file-access/authentication-log collectors are still reported as incomplete in the app.

The signed content archive `tmp/local/patch-posture-demo/three-areas-20261023/ubuntu-usn-signed.tar.gz` was imported visibly at **11:23:42 Africa/Lagos**. Bundle `controlone-local-ubuntu-usn-assessment`, version `2026.10.23.1`, sequence 1, content `selected-ubuntu-2404-advisories`, expires **30 October 2026 at 09:53:08 Africa/Lagos**. SHA-256 `604B99E0F48DDC5218A399E65A127971086C375598897084FA0EAC36857200CD`.

These are selected vendor mappings, not a comprehensive Ubuntu feed:

| Vendor advisory | Ubuntu 24.04 binary package | Fixed boundary | Lab before import |
| --- | --- | --- | --- |
| [USN-8945-1](https://ubuntu.com/security/notices/USN-8945-1) | `liblzma5` | `5.6.1+really5.4.5-1ubuntu0.4` | Installed `.3`: matched |
| [USN-8706-1 / CVE-2026-27171](https://ubuntu.com/security/notices/USN-8706-1) | `zlib1g` | `1:1.3.dfsg-3.1ubuntu2.2` | Already at fixed boundary: no match |
| [USN-8847-1 / CVE-2026-35189](https://ubuntu.com/security/notices/USN-8847-1) | `libssl3t64` | `3.0.13-0ubuntu3.16` | Already at fixed boundary: no match |

The xz notice has no assigned CVE in the reviewed notice. Its real **USN** identifier is stored in the existing generic string field named `cve_id`; no CVE number or severity/CVSS was invented. Severity is `unknown`. Local HTML source snapshots are retained with the evidence. This checks the recorded vendor fixed boundary, not the latest version forever.

Code changes add a required platform constraint when a feed specifies one. Inventory assessment supplies the node's reported OS, so these entries require `ubuntu 24.04`, `dpkg`, `amd64` and Debian version comparison. Missing/mismatched platform cannot match or authorize finding resolution. Broader distro identification and platform reporting remain future coverage work.

Historical finding reconciliation now permits a verified fixed current version to resolve the earlier vulnerable-version row, while retaining the original detection history. Resolution still requires the exact eligible advisory, package/source/architecture identity and explicit fixed boundary. A narrower feed, omitted advisory, malformed range, unavailable feed or newer observation cannot be cleared by this path. Focused tests and a restored isolated PostgreSQL database exercised these boundaries.

### Repeat the visible workflow and understand each step

1. Select **Local Development** in the top tenant selector. Open **Servers → phase0-ubuntu-lab → Packages**, turn off **Hide system packages** if needed and filter `liblzma5`. This is agent inventory, not an advisory lookup. Before this run it showed `5.6.1+really5.4.5-1ubuntu0.3`.
2. Open [Offline bundle](http://127.0.0.1:4173/console/offline-bundle), scroll to **Signed content bundles**, choose the signed archive and click **Import content**. This imports vulnerability definitions after signature validation; it does not install packages. Verify the Ubuntu bundle is **active**. Keep the content signing key separately trusted; a locally signed selection does not claim it was signed by Canonical.
3. Open **Patch posture** and refresh. **Assessment coverage is partial** is expected: two current vulnerability feeds are validated, but they do not cover every installed product. Tenant totals can include stale nodes; they are not this lab's complete health. The lab initially had one active `liblzma5` finding. The detailed finding/history is available from the tenant-scoped node vulnerabilities API; the present UI summary does not expose a full per-advisory findings table.
4. Before requesting an upgrade, an administrator checks APT metadata and simulates `apt-get -s --only-upgrade install liblzma5`. Here it showed **one upgrade, zero additions, zero removals**. Normal Ubuntu repository signature verification remained enabled. A temporary Squid proxy permitted only this lab, GET/HEAD and the official Ubuntu archive/security hosts; no host port was published. Administrative proxy assignment used the existing authenticated configuration API because the current UI cannot select a proxy ID in the per-node mode panel.
5. In **Patch posture → Deploy patches…**, retain **auto (per-node config)** after the administrator assigns the lab's proxy, enter `liblzma5` in **Package allowlist**, retain **Refresh package inventory after patching**, select **only phase0-ubuntu-lab**, enter the reason and submit. An empty allowlist is broader than this demonstration. Dependencies may also change, so review the simulation.
6. Open **Approvals** and approve this lab request. The first request was recorded as denied before execution; its failed history is retained. The user explicitly confirmed retry. The retry was requested at **11:37:53 Africa/Lagos** and approved through the app. Approval gates were retained.
7. Refresh and open **Per-node** on deployment `2d7d2a1c-cc47-4330-891e-cf716b157199`. It shows **COMPLETED, 1/1 applied**, with **APPLIED, PKGS 1**, no error, at **11:40:17 Africa/Lagos**. The log shows `.3` unpacked to `.4`. Job `111e1cf0-eb13-437f-9022-5bda0ac0edc0` used the existing agent APT execution path, not a manual replacement install.
8. Inspect refreshed packages. Independent full `dpkg-query` before/after lists confirm **only `liblzma5` changed**, to `5.6.1+really5.4.5-1ubuntu0.4`. Finding `4059f239-df8f-4e25-9b94-6cd39aeac4f7` is retained as resolved, with `resolution_basis: explicit_fixed_boundary`, the verified current version and observation time. Lab summary: **0 active, 1 resolved**. Other tenant findings remain; the aggregate dropped from four to three unresolved findings.
9. Restore the original **direct** patch configuration and stop the temporary repository proxy; both were done. Retain the denied request and successful retry as separate audit history. The retained proxy registration's historical `installing` status does not mean it is running or healthy.

Repeating this request now can legitimately upgrade zero packages: the fix is installed. For a fresh client demonstration use a separately identified disposable target with a known affected version and a tested checkpoint. Do not downgrade a real client host or run two enrolled copies with the same identity.

Screenshots: `signed-ubuntu-feed-active.png`, `liblzma-bounded-request.png`, `liblzma-applied-live.png`, `liblzma-inventory-after.png` in the dated evidence directory. Structured proof: `findings-before.json`, `findings-after.json`, `deployment-result.json`, `packages-before.tsv`, `packages-after.tsv`, `package-diff.json` and `liblzma-simulation.txt`.

## Signing and live trust evidence

The existing candidate compose override now runs the explicitly built `three-areas-20261023/controlplane-qualified`, SHA-256 `D3B108C69A0F6E769EF5E5F995A4BCC8138137F4827C7CE714A743274C72964D`. The API health endpoint returned 200 after startup. A first immediate health request encountered startup transience; the subsequent check passed.

The existing lab agent executable/service entrypoint was maintained, with original configuration/enrollment rollback copies retained. New agent **`phase0-signed-lab-20261023 (linux/amd64)`**, SHA-256 `32035203A8F8FD5B3B371CBBCB566AE7B9232D14182E9CA1546F1B9276BEB833`, reports a current heartbeat under the original node ID. Distinct local Ed25519 policy/release keys were configured on the server. Policy public key `/var/lib/control-one/nodeagent/keys/policy-pinned.pem` was provisioned administratively on the lab, independently of downloaded content.

Before installation, the lab's existing OpenSSL verified the binary's Ed25519 signature over its SHA-256 digest against independently provisioned `release-pinned.pem`. Downloaded agent code was not invoked to establish this first trust decision. The existing Bash online/offline templates now support `CONTROLONE_SIGNING_PUBLIC_KEY=/path/to/preprovisioned-public.pem`; when set they require signatures and verify using installed OpenSSL before executing the downloaded agent. Offline bundled keys cannot override that pin. Fixtures reject altered bytes, unsigned content and a bundle signed by its own attacker key before execution/installation; a valid independent pin verifies. Unsupported OpenSSL/key formats fail closed. Windows independent pinned bootstrap is not implemented by this Bash change.

The live lab repeatedly logs **policy signature verified**. Its signed cache was copied and independently verified with OpenSSL; altered-version, unsigned and wrong-key copies were rejected in isolation. The same container restarted successfully, verified signatures again and reported a current heartbeat under its existing ID; `signed-restart.txt` retains the log evidence. No live policy was replaced with negative-test data. The actual rule set is empty (zero policies), so this proves signed transport/cache verification of the set, not execution of an assigned security policy. Existing Linux/native Windows policy regression fixtures cover nonempty inert rules and missing/malformed keys.

Local TLS remains the existing lab proxy with a local CA; proxy-to-API and the Windows development connection still include HTTP. This is not managed production HTTPS. Development unsigned fallback remains when no pin is configured; merely adding a public key beside a download is insufficient. Replay/freshness, rotation, independently distributed managed anchors and signed-only enforcement across all approved platforms remain open.

Local signed-state recovery image: `controlone-phase0-patch-lab:20261023-signed-lib-lzma-qualified`, ID `sha256:b07788e2e1f682e4072538cceffdd469d441e41cee06c96bb2c925265a62757e`. The maintained lab compose file points to it so a later recreation does not silently restore the old unsigned agent/configuration. It is credential-bearing: keep private and never run a duplicate enrolled agent. The image was checkpointed locally; independent off-machine image recovery and full compose-recreation qualification remain open.

Tests are recorded in `trust-assessment-tests.txt` and `live-policy-proof.json`. Previous tests in the dated trust report remain relevant; this continuation supersedes its pending Linux live-rollout statement, not its production acceptance gates.

## Recovery checkpoint and acceptance rule

The initial checkpoint `recovery/controlone-three-areas-20261023.dump`, SHA-256 `CC5BD3264805068879AE0E5A45AD51A7B619FB2466295C9BAFBFBE2CDF0DCBD3`, restored successfully to an isolated database: tenants 36, nodes 26, deployments 3, findings 2. A separate restored database was used for PostgreSQL reconciliation tests. Neither test used the live database as its destructive fixture.

After the verified upgrade, a new custom-format dump `recovery/controlone-three-areas-final-20261023.dump` has SHA-256 `12D08A57329E484CC1C1639C372BC617FE53380B4D9C97CB7B52F073863429C4`. The application recovery package includes that dump, an online SQLite backup, server/compose configuration, the exact configured server/agent release binaries, local TLS material, separate policy/release/content keys, signed offline content and a native Git source delta (`base-commit.txt`, binary-capable working-tree patch and nonignored new source files). Source reconstruction also requires the recorded base commit; this is not a complete Git mirror. It excludes `.git`, arbitrary user directories and unrelated ignored files. These inputs contain credentials and must remain private.

The first integrity check over the Docker Windows bind mount was very slow. Only the task's temporary Python helper was stopped; the completed online SQLite copy was moved to private container storage and the checker resumed there. The live database/service was not replaced or stopped. PostgreSQL and SQLite are separate consistent checkpoints, not one coordinated cross-store point-in-time snapshot; full application recovery consistency and agreed RPO/RTO still need qualification.

Encryption/decryption, per-file hashes, SQLite integrity and isolated PostgreSQL restoration are recorded in `recovery-result.json`. The passphrase is stored separately from the encrypted archive in the ignored local task directory; keeping both on this machine is not external recovery. The enrolled lab image/state and Windows service configuration/spool are separate recovery assets; this application package does not assert complete endpoint or infrastructure disaster recovery.

- [x] AES-256 encrypted archive `three-areas-20261023/controlone-recovery-20261023.tar.gz.gpg`, **234,546,707 bytes**, SHA-256 **`AEDBE40BAAD6958DD8A1628AC793C60452BFFB16848C0FCEF86B9FA72AC9DBD3`**.
- [x] Decrypt into isolated private storage and verify **all 44 file hashes** and full SQLite integrity.
- [x] Restore the dump extracted from that decrypted archive, rather than reuse the source dump. Verify **36 tenants, 26 nodes, 5 deployments, 4 finding records**, with the lab finding resolved and direct patch mode retained.
- [x] Reject an incorrect passphrase cryptographically, with symmetric-key cache bypassed for the negative test. Stop the task-created GPG agent afterward.
- [x] Remove all three explicitly named disposable test databases and task-created plaintext restore/staging copies; retain original checkpoints, the private enrolled image, archive and separately stored local recovery secret.
- [ ] Copy to and restore from an approved destination outside this machine; qualify complete application startup, coordinated recovery consistency and the remaining recovery assets.

To complete the external gate: provision an approved removable disk, NAS or backup service outside this machine; copy the ciphertext and required private recovery assets/images/dependencies with protected access; escrow the recovery secret separately; retrieve from that destination into an isolated environment; verify ciphertext/file hashes, restore databases, rebuild/start the app with secrets safely provisioned and test tenant/enrollment/approval/audit state. Record timestamps, failure cases, owner and agreed RPO/RTO. Restoring the same on-host input does not satisfy that gate. A final resource check found approximately **0.96 GB RAM available**, only **C:** with **115.4 GB free**, and no mapped external destination.

Keep all four Phase 0 groups open until their remaining evidence is accepted. Phase 1 failure/concurrency/interruption/recovery qualification remains separate before a wider rollout. Cross-references: [review and phases](patch-posture-end-to-end-review.md), [baseline](patch-posture-local-demo-baseline.md), [package demonstrations](patch-posture-real-package-demo.md), [consolidated checklist](patch-posture-verification-2026-10-21.md), [trust review](patch-posture-trust-verification-2026-10-23.md).
