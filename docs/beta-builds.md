# Beta builds

Any pull request can be built into an installable beta you can try **from the
app on the speaker — no computer needed.** A beta is never built automatically;
a maintainer asks for one with a comment.

## How it works

Comment **`/beta`** on a pull request. The **Beta Build** workflow
(`.github/workflows/beta.yml`) then builds that PR's code — web UI first, then
the ARMv7 speaker binary — and publishes it as a GitHub *prerelease* tagged
`beta-pr-<number>`. The assets match a normal release (`retouch-armv7l` +
`SHA256SUMS`), so the speaker can fetch and verify it exactly like a stable
update. Comment `/beta` again to rebuild after new pushes; the beta is deleted
when the PR closes.

The build runs via `issue_comment` so a fork PR still gets a write token to
publish the release; for that reason it only acts on a `/beta` comment from an
**owner, member, or collaborator** and checks out the PR head ref explicitly.
This means betas are only ever produced on an explicit maintainer request —
opening or pushing to a PR never publishes anything on its own.

## Installing a beta from the app

On a speaker already running ReTouch:

1. Open the app and go to **Settings → Software**.
2. Under **Update**, open the version dropdown and pick the PR you want to test
   (listed as `PR #<n>: <title>` under *Beta builds*).
3. Tap **Install this beta**. ReTouch downloads, verifies the checksum, swaps its
   binary, and restarts — same as a normal update.

To return to the released version, pick **Latest (stable)** and update again.

## Installing a beta from the command line (optional)

You can still deploy a beta over SSH by pointing the installer at its tag:

```sh
RETOUCH_TARGET_TAG=beta-pr-30 sh install/install.sh <speaker-ip>
```
