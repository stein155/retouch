# Beta Builds

Use the **Beta Build** workflow to make an installable prerelease from a PR,
branch, tag, or commit SHA. It builds the web UI first, then embeds it into the
ARMv7 speaker binary and publishes the same assets as a normal release.

To build PR 30 from the command line:

```sh
gh workflow run beta.yml -f pr=30 -f tag=beta-pr-30
```

When the workflow is done, install that beta on a speaker with:

```sh
RETOUCH_TARGET_TAG=beta-pr-30 sh install/install.sh <speaker-ip>
```

If you omit `tag`, the workflow creates one like `beta-pr-30-<sha>`. Use that
generated tag as `RETOUCH_TARGET_TAG`.

To build a branch, tag, or commit instead of a PR:

```sh
gh workflow run beta.yml -f ref=main -f tag=beta-main-test
```

Beta releases are marked as prereleases and are not made the latest GitHub
release, so normal installs keep using the stable release.
