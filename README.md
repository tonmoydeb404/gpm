# GPM — Git Profile Manager

GPM manages multiple Git identities and SSH keys on a single machine.
It generates ED25519 keys, wires up `~/.ssh/config` host aliases and
`includeIf` rules in your global gitconfig, maps directories to
identities, verifies provider authentication, and guards repos against
committing with the wrong identity.

SSH profiles (keys + providers) and Git profiles (commit identities)
are independent entities — combine them freely.

Cross-platform (macOS/Linux), a single static binary, no external
dependencies beyond `git` and `ssh` themselves.

## Install

```sh
go install github.com/tonmoydeb404/gpm/cmd/gpm@latest
```

or build from source:

```sh
git clone https://github.com/tonmoydeb404/gpm && cd gpm
go build -o /usr/local/bin/gpm ./cmd/gpm
```

## Quickstart

```sh
# 1. A Git identity (commits as <username>; the email Git will use)
gpm profile add janedoe --email jane@corp.com

# 2. An SSH identity (generates an ED25519 key pair; the username is
#    the key comment)
gpm ssh add work --provider github.com --alias gpm-work

# 3. Copy the public key to your GitHub account
gpm key copy work        # paste at https://github.com/settings/keys

# 4. Verify authentication
gpm key test work        # → Authenticated as janedoe

# 5. Clone using the host alias
git clone git@gpm-work:<owner>/<repo>.git
```

Omitting `--alias` makes the profile the **global SSH identity** —
at most one profile can be global at a time, and global keys are not
wired into `~/.ssh/config`. With several providers the alias names
the first provider's stanza and the rest get an `<alias>-<host>`
suffix. The easy path is the combined onboarding in the TUI (`gpm` →
Create → Both Profile) or scripting the two `add` commands above.

## Folder-based identities

Map a directory to a Git profile and every repository below it picks
up the right identity automatically, via `includeIf` rules that GPM
manages in your global gitconfig:

```sh
gpm dir add ~/Works/corp janedoe
gpm dir add ~/Works/oss octocat      # nested mappings supported; deepest wins
```

A Git profile with no directories is the **global identity** — it
applies wherever no mapping matches. At most one profile can be
global at a time; gpm refuses changes that would create a second.

```sh
$ cd ~/Works/corp/repo
$ gpm current
Directory: ~/Works/corp/repo
Profile:   janedoe (directory mapping)
git name:  janedoe
git email: jane@corp.com
Identity:  OK (matches git config)
```

`gpm apply [username]` pins `user.name`/`user.email` into a repo's
local config; without an argument the profile is resolved from the
directory mapping.

## Wrong-identity protection

```sh
cd ~/Works/corp/repo
gpm guard install       # writes a pre-commit hook
```

Commits in mapped repos are blocked when `user.email` does not match
the mapped profile. Unmapped directories are never blocked, and the
hook fail-opens if `gpm` is uninstalled. Override a single commit with
`git commit --no-verify`.

## Diagnostics

```sh
gpm doctor              # profiles, keys, permissions, ssh/gitconfig, mappings, repo identity
gpm doctor --fix        # auto-repair permissions, missing/stale managed blocks
gpm doctor --network    # also test live GitHub auth for every key
```

Doctor exits non-zero when any check fails, so it can gate scripts.

## Importing an existing setup

Already juggling accounts with hand-written ssh config?

```sh
gpm scan               # read-only report of what was detected
gpm import --dry-run   # see what would be imported
gpm import             # confirm and import: profiles + directory mappings
```

`scan` inspects three sources and reports them without writing
anything:

- `~/.ssh/config` — Host stanzas with identity files, including
  `Include`d files; effective keys are resolved with `ssh -G`
- your global gitconfig — `include` and `includeIf` chains are
  followed recursively across `~/.gitconfig` and
  `~/.config/git/config`
- git repositories on disk — a depth-limited walk of your home
  directory (`--dir` to change the root, `--depth` to tune it,
  `--no-repos` to skip) reads each repo's local identity and remote

Repos connect the pieces: a repo cloned through a host alias adopts
that account's email, and its directory joins the proposed mapping.
Repos whose identity matches no account are listed separately.

## Interactive TUI

Run `gpm` with no arguments (in a terminal) to open the interactive
menu:

```sh
gpm
```

Navigation is keyboard-first and the same on every screen:
`↑/↓` move, `enter` select, `esc` back, `q` quit.

- `GPM`
  - `SSH Profiles` — create profiles, update/delete, view & copy the
    public key, test the connection, manage providers
  - `Git Profiles` — create profiles, update/delete, manage their
    directory mappings
  - `Create` — `Both Profile` (git identity + SSH key in one flow),
    `SSH Profile`, or `Git Profile`
  - `Run Doctor` — diagnose the setup
  - `Scan & Import` — detect an existing setup and migrate it
  - `Exit`

Creation forms only ask for what matters (name, comment, provider,
email, directory) and explain every field inline. After creating an
SSH key the success screen shows the public key, explains the next
step, and offers copy/test shortcuts.

Non-TTY invocations (scripts, CI) print help instead of launching the
interface. All changes made interactively go through the same
internal packages and sync pipeline as the CLI.

## Command overview

| Command | Purpose |
|---|---|
| `gpm profile add/list/show/edit/remove` | Manage Git identities |
| `gpm ssh add/list/show/remove` | Manage SSH identities |
| `gpm ssh provider add/remove` | Manage provider hostnames of an SSH identity |
| `gpm key generate/list/show/copy/remove/test` | Manage and verify SSH keys |
| `gpm dir add/remove/list` | Directory → Git identity mappings |
| `gpm current [--dir]` | Show the active profile and identity |
| `gpm apply [username]` | Pin an identity into the current repo |
| `gpm sync` | Regenerate all managed config |
| `gpm doctor [--fix] [--network]` | Diagnose and repair |
| `gpm scan [--dir] [--depth] [--no-repos]` | Read-only report of an existing setup |
| `gpm import [--dry-run]` | Adopt an existing multi-account setup |
| `gpm guard install/remove` | Pre-commit wrong-identity protection |
| `gpm completion bash\|zsh\|fish\|powershell` | Shell completions |

All commands work non-interactively (flags in, JSON-free text out) and
prompt interactively only when a TTY is attached and required flags
are missing.

## How GPM touches your system

- `~/.config/gpm/config.toml` — GPM's own state (git + ssh profiles
  with their directories/providers)
- `~/.config/gpm/gitconfig/<username>` — generated per-profile `[user]` config
- `~/.ssh/config` — one `# BEGIN/END GPM MANAGED BLOCK` section with Host
  aliases (one per SSH profile × provider); everything outside the
  markers is never touched
- `~/.gitconfig` — one managed block with `includeIf` rules
- `~/.config/gpm/backups/` — timestamped backups (last 10 per file) taken before any modification

## Security

- ED25519 keys only, OpenSSH format, generated locally
- Private keys: `0600`, public keys: `0644`, `~/.ssh`: `0700`
- Private keys never leave the machine; nothing is uploaded
- No passwords or tokens stored; GitHub verification uses plain SSH

## Roadmap

- GitLab / Bitbucket / self-hosted providers
- `gpm clone` (clone with the right profile automatically)
- GitHub API integration and automatic key registration
- Configuration sync, Windows support

## Development

```sh
go build ./cmd/gpm       # build
go test ./...            # unit tests (run in an isolated fake $HOME)
go vet ./... && gofmt -l .
```

### Sandbox (dev) mode

Set `GPM_HOME` to try gpm out without touching your real setup. Every
path gpm reads or writes — its config, `~/.ssh`, `~/.gitconfig`, `~`
expansion, even the global gitconfig git sees and the known_hosts ssh
uses — is redirected under the sandbox:

```sh
export GPM_HOME=/tmp/gpm-dev

gpm profile add janedoe --email jane@corp.com
gpm ssh add work --provider github.com
gpm key list
gpm doctor
gpm current
```

gpm prints a sandbox notice to stderr while `GPM_HOME` is set, and
nothing outside that directory is modified. For a full end-to-end
sandbox that also covers your own `git`/`ssh` invocations, override
`HOME` in a throwaway shell instead:

```sh
env HOME=$(mktemp -d) gpm doctor
```

## License

MIT
