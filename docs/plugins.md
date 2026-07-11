# ReTouch plugins

ReTouch can host **plugins**: separate binaries — each with its own GitHub release
and its own dependencies — that add features to an already-installed speaker without
rebuilding or reflashing ReTouch. The first plugin is `retouch-ring` (a Ring chime).

## How it works

- Plugins are stored under `<home>/plugins/<name>/` and recorded in `plugins.json`.
- ReTouch downloads a plugin from its GitHub release and verifies it exactly like its
  own OTA: the binary must match its `SHA256SUMS` entry, and when the catalog entry
  carries an ed25519 public key, `SHA256SUMS` must also carry a valid `.sig`
  (`internal/release`). A binary can also be **sideloaded** (uploaded through the UI)
  while its release repo is still private — this skips verification, so the endpoint
  is disabled unless ReTouch was started with **`-allow-sideload`** (an unverified
  upload is, in effect, run-arbitrary-code for anyone on the LAN; the flag can't be
  flipped at runtime for exactly that reason).
- ReTouch runs each plugin as a **supervised child process** (restart-on-crash), so
  plugins need no autostart of their own: they live and die with ReTouch and come
  back on reboot because ReTouch does. Removing a plugin stops the child and deletes
  its directory — nothing else to undo.
- ReTouch launches a plugin with `--speaker-host <host:port> --config-dir <dir>
  --listen 127.0.0.1:<port> --host-url <retouch base url>` and **reverse-proxies**
  `/api/plugins/<name>/…` to the plugin's loopback port, behind the same guard
  (DNS-rebinding + CSRF + body cap) as the rest of the API. The proxy only starts
  forwarding once the plugin's `/health` has answered.
- The child's **stdin is a pipe** whose write end ReTouch holds for its lifetime. A
  plugin should exit when stdin hits EOF: that covers every way ReTouch can die —
  including the self-update's `os.Exit` and a crash — so no orphan survives to run
  next to the relaunched ReTouch. (ReTouch also stops plugins explicitly before a
  self-update restart; the pipe is the belt to that suspender.)

## The contract a plugin must implement

A plugin serves a small HTTP API on its `--listen` address:

| Method & path        | Purpose |
|----------------------|---------|
| `GET  /health`       | Liveness for the host (any 200 is healthy). |
| `GET  /manifest`     | The current settings UI as a server-driven schema (below). |
| `POST /action/{id}`  | Perform an action; return the **next** manifest. |

ReTouch's frontend renders the manifest generically — it ships **no plugin-specific
code**. That is what lets a plugin be added to an installed speaker without rebuilding
ReTouch's embedded UI.

### Manifest schema

```jsonc
{
  "title": "Ring chime",
  "status": { "level": "idle|ok|warn|error", "text": "Connected to Ring" },
  "sections": [
    {
      "title": "Ring account",
      "text": "optional descriptive paragraph",
      "fields": [
        { "key": "email",    "label": "Email",    "type": "text" },
        { "key": "password", "label": "Password", "type": "password" },
        { "key": "code",     "label": "Code",     "type": "otp" }
      ],
      "rows": [
        { "id": "111", "label": "Front Door",
          "toggles": [ { "key": "motion", "label": "Motion", "value": true },
                       { "key": "ding",   "label": "Doorbell", "value": true } ] }
      ],
      "actions": [ { "id": "login", "label": "Log in", "style": "primary", "confirm": "…?" } ]
    }
  ]
}
```

Field `type` is `text | password | number | otp | toggle | select`. A `select` field carries `"options": [ { "value": "…", "label": "…" } ]`; on older hosts it degrades to a text input. Action `style` is
`primary | danger | ""`.

### Actions return the next manifest — so multi-step flows are free

Every action POSTs the current inputs and receives the manifest to render next:

```jsonc
POST /action/login   { "values": { "email": "…", "password": "…" },
                       "rows":   { "111": { "motion": true, "ding": false } } }
→ 200  <the next manifest>          // e.g. one that now shows a "code" field for 2FA
→ 400  { "error": "human-readable message" }   // shown under the section; state unchanged
```

**Login → 2FA → connected** needs no special-casing in ReTouch: it is just the
sequence of manifests the plugin returns. `retouch-ring` uses exactly this — see its
`plugin` package.

## Adding a plugin to the catalog

`internal/plugins.Catalog()` is the curated list ReTouch offers. Each entry:

```go
CatalogEntry{ Name, Title, Description, Repo /* owner/name */, Asset /* armv7l */, PubKey /* base64 ed25519, optional */ }
```
