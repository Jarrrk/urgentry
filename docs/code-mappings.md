# Code mappings

Code mappings turn file paths in stack frames into links to the matching source files in a repository. They do not upload, clone, or fetch source code.

For each frame, Urgentry finds the first mapping whose **Stack Root** is a prefix of the frame filename. It removes that prefix, prepends **Source Root**, and creates this URL:

```text
{Repository URL}/blob/{Default Branch}/{Source Root}{remaining frame path}#L{line}
```

For example, given:

| Field | Value |
|---|---|
| Stack Root | `/srv/highlife/` |
| Source Root | `services/game/` |
| Repository URL | `https://github.com/example/platform` |
| Default Branch | `main` |

the frame `/srv/highlife/client/errors.lua:42` links to:

```text
https://github.com/example/platform/blob/main/services/game/client/errors.lua#L42
```

## Field reference

- **Stack Root** is the path prefix reported by the runtime. Use an empty value to match every frame.
- **Source Root** is the directory containing that code inside the repository. It may be empty when runtime paths are already repository-relative.
- **Repository URL** is the repository's browser URL without a trailing source-file path.
- **Default Branch** is used for links when an event does not identify a commit. It defaults to `main`.

Mappings are evaluated in their stored order and the first match wins. Use the most specific stack roots when a project has multiple mappings.

## Private repositories and authentication

Urgentry does not make authenticated requests to the repository, so there is no repository access token to configure and no token is stored. A private-repository link works when the person opening it already has an authenticated browser session with the Git forge.

Do not put an access token in the Repository URL. It could be exposed in rendered links, logs, browser history, or copied URLs.

The current link format uses `/blob/{branch}/{path}`, as supported by GitHub and GitLab. Forgejo and Gitea normally use `/src/branch/{branch}/{path}` instead, so their source links are not currently generated correctly. Supporting those forges requires a provider-aware or configurable URL template; access-token authentication is still unnecessary for browser links.
