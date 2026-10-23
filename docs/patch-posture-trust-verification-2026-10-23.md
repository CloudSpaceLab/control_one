# Phase 0 trust qualification — 23 October 2026

**Later live qualification:** [the three-area report](patch-posture-three-areas-verification-2026-10-23.md#signing-and-live-trust-evidence) supersedes the earlier pending Linux rollout below. Qualified server/Linux binaries are deployed under the existing enrollment; the release was independently verified before installation, and the live signed policy cache verifies against the administratively pinned key. Altered, unsigned and wrong-key cache copies are rejected in isolation. The existing Bash installers now support an independent OpenSSL verification pin before downloaded-code execution. Production HTTPS, Windows independent bootstrap, enforced signed-only distribution, replay/freshness and rotation remain open; these Linux results do not close the Trust group.

This work belongs to **item 2: Trust** in the original four Phase 0 evidence groups. It does not complete production HTTPS, signing-key ownership/distribution or fleet qualification. The branch remains `0y3/Me/patch-posture-review`; no new `.ps1` source file is introduced.

## Problems and changes

1. An explicitly configured but missing policy verification key previously disabled signature verification. It now returns an initialization error, like malformed/unreadable configured keys. The existing agent startup path treats that as fatal: provision and check the key before rollout. Intentionally empty key configuration still permits the existing unsigned development mode with its warning; that mode does not qualify for production trust.
2. Cached compliance policies previously bypassed signature verification. Cached sets now undergo the same verification as downloaded sets when a trust key is configured. Invalid downloads are rejected before persistence, preserving the last trusted cache. Invalid cache content is rejected, and the existing scanner path skips the compliance scan rather than executing those rules.
3. Online/offline installers previously warned and continued after verifier failure. Supplied signatures must now verify before the install/copy step. PowerShell checks `$LASTEXITCODE` because a nonzero native process exit is not reliably caught by `try/catch`.
4. Signed offline bundles previously omitted the verifier's required public-key input. They now carry `signing-public-key.pem`, including when the server derives the public key from its private key, and both existing offline templates pass `--public-key`. Missing declared signatures or signing-key files stop the signed offline path.
5. Online Bash signature checking no longer depends on finding `sha256sum`; checksum verification can also use `shasum -a 256`, preserving the macOS path. Missing checksum tooling for a supplied checksum stops installation.

## Verification and scope

- [x] Policy trust tests pass on Linux and native Windows: valid fetch/cache, unsigned content, altered rules/version, wrong key, malformed signature and missing/malformed configured keys. A rejected update retains a usable previously signed cache.
- [x] Signed offline bundle tests pass for expected contents, matching detached signature/key and rejection of altered binary bytes; existing Linux/Windows/macOS bundle response tests and online installer rendering checks pass.
- [x] The existing Bash offline installer stops before creating install/config directories in all three isolated cases: verifier exit 7, missing declared signature and missing public-key file.
- [x] Native PowerShell parses both embedded installer templates without creating `.ps1` files. The actual online verification block rejects a nonzero native verifier exit before any install block is executed. This does not claim a complete signed Windows installation/reboot test.
- [x] Existing enrollment bootstrap regression passes on Linux: omitted/explicit-empty key configuration remains compatible, a missing explicit key is rejected, and a valid key initializes successfully.

Evidence: `tmp/local/patch-posture-demo/verification-run/trust-server-tests-20261023.txt` and `trust-policy-bootstrap-tests-20261023.txt`. The first native Windows policy run hit sandbox loopback restrictions; the same tests passed with the required local socket access. No firewall, ACL or certificate-validation setting was weakened.

Runtime inspection found the control-plane container using `go run ./controlplane/cmd/controlplane --config tmp/local/controlplane.yaml`, rather than the saved 21 October candidate executable. Source changes or file hashes alone cannot prove which of today's fixes its already-running process contains. No enrolled service was replaced in this continuation; agent rollout and live signed-mode verification remain pending. The saved candidate compose override is still available, but this report does not treat an unrelated source rebuild/restart as completed trust deployment.

## Live lab connectivity repair

The recreated control-plane container had only `control-one-local_default`, with its internal lab network missing. Windows remained connected, but Ubuntu's last heartbeat was **23 October at 08:36:07 Africa/Lagos**. Reattached the API to the existing `controlone-phase0-agent-internal` network with alias `phase0-controlplane`, validated the existing nginx configuration and reloaded it using `-c /etc/controlone-tls/nginx.conf`. The first reload without that custom config looked for the wrong PID file and failed; the correctly targeted reload succeeded. Internal health then passed and Ubuntu resumed reporting under its original enrollment.

The existing `scripts/local.ps1` now restores this optional, already-provisioned lab connection after `compose up`, and reloads the existing lab TLS proxy so it resolves the current API address. Fresh setups without that network keep their existing topology. No new PowerShell script was created. PowerShell syntax and the idempotent recovery block were exercised against the running lab, without restarting the API/UI. Full launcher-driven container recreation was not repeated during this check.

In the visible app, **Local Development → Servers → phase0-ubuntu-lab → Overview** showed **CONNECTED · HEARTBEAT CURRENT**, with a last-seen time of **09:05:34 Africa/Lagos**. Screenshot: `tmp/local/patch-posture-demo/verification-run/ubuntu-connectivity-recovered-20261023.png`. File-access and authentication-log coverage remain unsupported in this minimal container; the historical predictive score is not evidence that those gaps are fixed or that the new trust code is deployed.

The policy fixtures sign synthetic inert rule strings using temporary Ed25519 keys. They test valid, unsigned, altered rule, altered version, wrong key and malformed-signature cases for fetch and cache. These are isolated tests, not assignments of new shell policies to the enrolled Windows/Ubuntu nodes.

Installer fixtures exercise the existing template in temporary directories with a verifier stub that exits unsuccessfully; they prove stop-before-install behavior, not the authenticity of a stub. Separate Go bundle checks independently verify the detached signature against the bundled key and reject altered binary bytes. Native PowerShell checks parse both embedded templates without creating `.ps1` files and exercise the online block's nonzero native-exit handling without executing installation.

## Next live signed-mode demonstration

1. Record the running server and target executable/configuration fingerprints; retain rollback copies and reconcile the active launcher with the candidate compose override.
2. Establish the intended trusted signing public key independently of the downloaded binary/bundle. Protect the private key and specify its owner and recovery/rotation process.
3. On the disposable lab target, provision that public key, check its ownership/readability and configure the existing `policy.public_key_file`. Use the existing installer/service rather than a parallel enrollment or new `.ps1` script.
4. Deploy the reviewed agent/server candidates, fetch a correctly signed policy set and verify the cached set against the configured key. Keep a fresh heartbeat under the same identity.
5. Repeat unsigned/tampered/wrong-key/replay cases using an isolated target or cache copy. Do not overwrite live policies with negative-test content. The first three rejection classes are proven in fixtures here; replay/freshness is not yet qualified.
6. Verify service restart and recovery with the retained signed policy/key. Roll back using the existing service and preserved state if needed, while recording that reverting code restores its previous weaknesses.

Full production acceptance also requires the independent bootstrap and HTTPS gates below. An app inventory card or a passing checksum is not evidence that this signing chain has been deployed.

## Remaining acceptance gates

- [x] Deploy a deliberately keyed Linux lab agent/server and independently verify its live signed cache, including isolated negative copies; completed in the later three-area continuation.
- [ ] Complete deliberately keyed-agent qualification for Windows and the full supported deployment scope; the Linux lab result does not convert other enrollments.
- [x] Add and test independent pinned OpenSSL bootstrap verification in existing Bash installers and independently verify the lab release before installation. This mode requires an externally provisioned pin.
- [ ] Establish managed authenticated trust-anchor distribution across the supported platforms, including independent Windows bootstrap. Development fallback remains; this is not mandatory production signed-only enforcement.
- [ ] Enforce the approved production signed-only mode. Unsigned development fallback still exists, including manifest/download fallback paths; stripping all signature metadata is not qualified by the supplied-signature failure tests.
- [ ] Trusted, reachable HTTPS across the intended deployment path, certificate renewal, protected signing keys and key-rotation/recovery ownership.
- [ ] Policy replay/version/freshness and key-rotation qualification. A valid signature alone does not establish that a policy is the latest approved version.
- [ ] Full-host Ubuntu VM tests. Available RAM was about **0.49 GB** at the start of this continuation; the prepared VM remained powered off. No unrelated workload was terminated to free RAM.

External recovery and broader vulnerability assessment remain separate open groups. See [the consolidated checklist](patch-posture-verification-2026-10-21.md#current-checklist-and-closure-rule).
