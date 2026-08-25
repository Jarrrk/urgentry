# Code mappings

Code mappings turn file paths in stack frames into links to matching source files. For Forgejo and Gitea, Urgentry can also fetch a small source window around the failing line without cloning the repository.

For each frame, Urgentry finds the first mapping whose **Stack Root** is a prefix of the frame filename. It removes that prefix, prepends **Source Root**, and creates the URL format selected by **Repository Provider**:

```text
GitHub/GitLab: {Repository URL}/blob/{Default Branch}/{Source Root}{remaining frame path}#L{line}
Forgejo/Gitea: {Repository URL}/src/branch/{Default Branch}/{Source Root}{remaining frame path}#L{line}
```

For example, given:

| Field | Value |
|---|---|
| Stack Root | `/srv/highlife/` |
| Source Root | `services/game/` |
| Repository Provider | `GitHub` |
| Repository URL | `https://github.com/example/platform` |
| Default Branch | `main` |

the frame `/srv/highlife/client/errors.lua:42` links to:

```text
https://github.com/example/platform/blob/main/services/game/client/errors.lua#L42
```

For a Forgejo repository at `https://forge.hlf.is/HighLife/core`, where the frame is `highlife/client/core/error.lua:8` and the source lives under `[highlife]/highlife/`, use:

| Field | Value |
|---|---|
| Stack Root | `highlife/` |
| Source Root | `[highlife]/highlife/` |
| Repository Provider | `Forgejo` |
| Repository URL | `https://forge.hlf.is/HighLife/core` |
| Default Branch | `master` |

This produces:

```text
https://forge.hlf.is/HighLife/core/src/branch/master/%5Bhighlife%5D/highlife/client/core/error.lua#L8
```

## Field reference

- **Stack Root** is the path prefix reported by the runtime. Use an empty value to match every frame.
- **Source Root** is the directory containing that code inside the repository. It may be empty when runtime paths are already repository-relative.
- **Repository Provider** selects the forge's source-link format.
- **Repository URL** is the repository's browser URL without a trailing source-file path.
- **Default Branch** is used for links when an event does not identify a commit. It defaults to `main`.

Mappings are evaluated in their stored order and the first match wins. Use the most specific stack roots when a project has multiple mappings.

## Forgejo source context and private repositories

Browser links to private repositories use the viewer's existing Forgejo session. To display source context inside Urgentry, configure the trusted Forgejo origin and a token with read-only repository access:

```text
URGENTRY_FORGEJO_URL=https://forge.hlf.is
URGENTRY_FORGEJO_TOKEN_FILE=/etc/urgentry/forgejo-token
```

`URGENTRY_FORGEJO_TOKEN` may be used instead of the file setting, but a root-readable token file is preferable for systemd. Restrict the token to the required repository and grant only `read:repository`. Urgentry sends it only when the mapping's repository URL has the same origin as `URGENTRY_FORGEJO_URL`. Source files larger than 2 MiB are not loaded.

Do not put an access token in the Repository URL. It could be exposed in rendered links, logs, browser history, or copied URLs. Tokens are not stored in the Code Mapping database row.

Select **Forgejo** or **Gitea** as the Repository Provider to generate their `/src/branch/{branch}/{path}` source links. GitHub and GitLab use `/blob/{branch}/{path}` links instead. Access-token authentication is unnecessary for either format because the links open in the user's browser.
