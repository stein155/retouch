# Release signing

The agent's self-update downloads `retouch-armv7l` and `SHA256SUMS` over HTTPS and
checks the binary against the checksum. TLS proves the files came from GitHub and
the checksum proves the binary wasn't truncated — but both the binary and the
checksum come from the *same* GitHub release, so anyone who can publish a release
(a compromised account or CI) can ship a matching pair. That binary runs as **root**
on the speaker.

Signing closes that gap: CI signs `SHA256SUMS` with an ed25519 **private** key, and
the agent verifies the signature with the **public** key compiled into it. An
attacker who can publish a release still can't forge the signature.

**Status: enabled.** A public key is compiled into `releasePublicKey`
(`internal/web/web.go`), so agents built from this commit onward refuse a real
release whose `SHA256SUMS` isn't validly signed.

> **Required before the next release:** the `RELEASE_SIGNING_KEY` repository secret
> (the base64 private key) must be set, or the release workflow won't attach
> `SHA256SUMS.sig` and agents already upgraded to a signed build will be unable to
> install the next release. See step 3 below.

**Betas are exempt.** The Beta Build workflow builds untrusted PR code and must
never be given the signing key, so betas ship no `SHA256SUMS.sig`. The agent skips
signature enforcement for `beta-pr-<n>` tags (they stay maintainer-gated, opt-in
prereleases over TLS + checksum); see `installRelease` in `internal/web/web.go`.

## One-time setup

Steps 1–2 are already done for the active key (the public half is in
`releasePublicKey`). **Step 3 is the outstanding action**: set the
`RELEASE_SIGNING_KEY` secret to the base64 of the matching private key. The steps
below are the full reference (also used when rotating the key):

1. Generate a keypair (keep `ed25519.pem` secret; never commit it):

   ```sh
   openssl genpkey -algorithm ed25519 -out ed25519.pem
   ```

2. Compile the **public** key into the agent — put its base64 into
   `releasePublicKey` in `internal/web/web.go`:

   ```sh
   openssl pkey -in ed25519.pem -pubout -outform DER | tail -c 32 | base64
   ```

   (ed25519 public keys are 32 bytes; that's the raw key at the end of the DER
   SPKI, which is what `verifyReleaseSignature` expects.)

3. Add the **private** key as a repository secret named `RELEASE_SIGNING_KEY`,
   base64-encoded so it survives as a single line:

   ```sh
   base64 -w0 ed25519.pem
   ```

   Settings → Secrets and variables → Actions → New repository secret.

Once both the constant is non-empty and the secret is set, the next release
publishes `SHA256SUMS.sig` (see `.github/workflows/release.yml`), and agents built
from that commit require a valid signature before installing an update.

## Rotating / disabling

- **Rotate:** generate a new keypair, update `releasePublicKey`, replace the secret.
  Agents must be updated to the release carrying the new public key *before* the old
  key is retired (they verify with the key they were built with).
- **Disable:** set `releasePublicKey` back to `""`. The agent falls back to
  TLS + checksum only.
