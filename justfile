# justfile for mongodb-mcp
# Run `just` to see all available commands.

set shell := ["bash", "-euo", "pipefail", "-c"]

# Default — list recipes.
default:
    @just --list --unsorted

# ─────────────────────────── Build & Test ───────────────────────────

# Version baked into the binary at link time.
GO_LDFLAGS := "-s -w -X main.version=$(git describe --tags --always --dirty 2>/dev/null || echo dev)"

# Build the binary into ./bin/.
build:
    mkdir -p bin
    go build -ldflags="{{GO_LDFLAGS}}" -o bin/mongodb-mcp ./cmd/mongodb-mcp
    @echo "Built ./bin/mongodb-mcp"

# Install into $GOBIN (or $GOPATH/bin).
install:
    go install -ldflags="{{GO_LDFLAGS}}" ./cmd/mongodb-mcp
    @echo "Installed mongodb-mcp to $(go env GOBIN || echo $(go env GOPATH)/bin)"

fmt:
    golangci-lint fmt ./...

# Point git at .githooks/ so the pre-commit format gate fires. One-shot
# per clone; idempotent. CI still runs the same check as the
# authoritative gate — the hook just catches drift earlier.
install-hooks:
    #!/usr/bin/env bash
    set -euo pipefail
    git config core.hooksPath .githooks
    echo "git config core.hooksPath = .githooks"
    echo "pre-commit golangci-lint fmt gate is now active (bypass with --no-verify)."

# Auto-fix formatting drift, then vet + the full golangci-lint gate.
# Anything that *can* be regenerated *is* regenerated. CI uses the
# read-only `lint-check` variant as the strict gate so a broken `just
# lint` never silently re-fixes the CI workspace.
lint: fmt
    go vet ./...
    golangci-lint run ./...

# Strict read-only check — same logic CI runs, exposed for local pre-push
# verification. Fails if formatting would change or any linter fires.
lint-check:
    #!/usr/bin/env bash
    set -euo pipefail
    out=$(golangci-lint fmt --diff ./...)
    if [ -n "$out" ]; then
        echo "code is not formatted; run 'just fmt':"
        printf '%s\n' "$out"
        exit 1
    fi
    go vet ./...
    golangci-lint run ./...

test:
    go test ./...

# Integration tests against a real MongoDB spun up via testcontainers.
# Requires Docker; pulls the mongo:7 image on first run.
test-integration:
    go test -tags=integration -timeout 10m ./...

# Full local gate: lint + unit tests + regenerate-and-verify schema,
# docs and flake are in sync.
check: lint test sync-schema sync-docs sync-flake

# ─────────────────────────── Generated artifacts ───────────────────────────

# Regenerate config.schema.json from the config.Config Go types. Cheap
# (pure reflection) so we run it on every `just check`. CI asserts no
# drift on PRs and auto-commits on master pushes.
sync-schema:
    #!/usr/bin/env bash
    set -euo pipefail
    go run ./cmd/gen-schema -out config.schema.json
    if [ -n "$(git status --porcelain config.schema.json)" ]; then
        echo "sync-schema: regenerated config.schema.json"
    else
        echo "sync-schema: schema already in sync"
    fi

# Regenerate the gomarkdoc API docs under docs/. Catches doc drift in
# PRs by comparing the rewrite against what's in git.
sync-docs:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p docs
    tmpl='docs/{{ "{{" }}.Dir{{ "}}" }}.md'
    if command -v gomarkdoc >/dev/null 2>&1; then
        gomarkdoc --output "$tmpl" ./...
    else
        go run github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest --output "$tmpl" ./...
    fi
    if [ -n "$(git status --porcelain docs)" ]; then
        echo "sync-docs: regenerated docs/"
    else
        echo "sync-docs: docs already in sync"
    fi

clean:
    rm -rf bin/ result result-*

# ─────────────────────────── Nix ───────────────────────────

nix-build:
    nix build .#default --print-build-logs

nix-check:
    nix flake check --print-build-logs

# Keep flake.nix's `vendorHash` aligned with the current go.sum.
#
# A sha256 of go.sum is embedded as a `# go-sum:` line in flake.nix.
# When the cached digest matches go.sum on disk, sync-flake returns
# immediately without running `nix build`. That makes it cheap enough
# to run on every `just check`, so a dev `go get` flow can never push a
# commit that breaks nix CI.
#
# Pass `--force` to bypass the cache and re-run the nix build even if
# go.sum looks unchanged. (The version string is driven by the VERSION
# file, so version bumps go there — see the release recipes.)
sync-flake force="":
    #!/usr/bin/env bash
    set -euo pipefail
    FORCE=0
    [ "{{force}}" = "--force" ] && FORCE=1

    GO_SUM_HASH=$(sha256sum go.sum | awk '{print $1}')
    CACHED_HASH=$(awk -F': ' '/^[[:space:]]*#[[:space:]]*go-sum:/ {print $2; exit}' flake.nix | tr -d ' ')

    if [ "$FORCE" = "0" ] && [ "$GO_SUM_HASH" = "$CACHED_HASH" ]; then
        echo "sync-flake: up-to-date (go.sum=$GO_SUM_HASH)"
        exit 0
    fi

    echo "sync-flake: refreshing vendorHash"
    SENTINEL="sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
    sed -i -E 's|^(\s*vendorHash = )"sha256-[^"]*";|\1"'"$SENTINEL"'";|' flake.nix
    set +e
    OUT=$(nix build .#default --no-link 2>&1)
    BUILD_STATUS=$?
    set -e
    NEW_HASH=$(printf '%s\n' "$OUT" | awk '/got:[[:space:]]*sha256-/ {print $2; exit}')
    if [ -z "$NEW_HASH" ]; then
        if [ "$BUILD_STATUS" = "0" ]; then
            echo "sync-flake: unexpected nix build success with sentinel hash" >&2
        fi
        printf '%s\n' "$OUT" >&2
        echo "sync-flake: nix build failed without printing 'got: sha256-…'" >&2
        exit 1
    fi
    sed -i -E 's|^(\s*vendorHash = )"sha256-[^"]*";|\1"'"$NEW_HASH"'";|' flake.nix
    if grep -q '^[[:space:]]*# go-sum:' flake.nix; then
        sed -i -E 's|^(\s*# go-sum:).*|\1 '"$GO_SUM_HASH"'|' flake.nix
    else
        sed -i -E 's|^(\s*vendorHash = )|          # go-sum: '"$GO_SUM_HASH"'\n\1|' flake.nix
    fi
    echo "sync-flake: vendorHash=$NEW_HASH go-sum=$GO_SUM_HASH"

    # Hard guard: never leave the sentinel behind.
    if grep -q "$SENTINEL" flake.nix; then
        echo "sync-flake: refusing to leave sentinel vendorHash in flake.nix" >&2
        exit 1
    fi
    nix build .#default --no-link

# ─────────────────────────── Release ───────────────────────────

release-preview:
    #!/usr/bin/env bash
    set -euo pipefail
    CURRENT_TAG=$(git tag -l 'v*.*.*' --sort=-v:refname | head -1)
    CURRENT_TAG=${CURRENT_TAG:-v0.0.0}
    CURRENT_VERSION=${CURRENT_TAG#v}
    MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
    MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
    PATCH=$(echo "$CURRENT_VERSION" | cut -d. -f3)
    echo "Current tag: $CURRENT_TAG"
    echo "  release-major: v$((MAJOR + 1)).0.0"
    echo "  release-minor: v${MAJOR}.$((MINOR + 1)).0"
    echo "  release-patch: v${MAJOR}.${MINOR}.$((PATCH + 1))"

_release-checks:
    #!/usr/bin/env bash
    set -euo pipefail
    BRANCH=$(git rev-parse --abbrev-ref HEAD)
    DEFAULT_BRANCH=$(git rev-parse --abbrev-ref origin/HEAD 2>/dev/null | sed 's|^origin/||' || true)
    if [ -z "${DEFAULT_BRANCH:-}" ]; then
        DEFAULT_BRANCH=$(git remote show origin 2>/dev/null | awk '/HEAD branch/ {print $NF}' || echo main)
    fi
    if [ "$BRANCH" != "$DEFAULT_BRANCH" ]; then
        echo "Error: not on default branch '$DEFAULT_BRANCH' (currently '$BRANCH')." >&2
        exit 1
    fi
    just check
    if [ -n "$(git status --porcelain)" ]; then
        echo "Generated/format changes — staging + committing."
        git add -A
        git commit -m "chore: regenerate artifacts for release"
    fi

_release bump:
    #!/usr/bin/env bash
    set -euo pipefail
    just _release-checks
    CURRENT_TAG=$(git tag -l 'v*.*.*' --sort=-v:refname | head -1)
    CURRENT_TAG=${CURRENT_TAG:-v0.0.0}
    CURRENT_VERSION=${CURRENT_TAG#v}
    MAJOR=$(echo "$CURRENT_VERSION" | cut -d. -f1)
    MINOR=$(echo "$CURRENT_VERSION" | cut -d. -f2)
    PATCH=$(echo "$CURRENT_VERSION" | cut -d. -f3)
    case "{{bump}}" in
        major) NEW="$((MAJOR + 1)).0.0" ;;
        minor) NEW="${MAJOR}.$((MINOR + 1)).0" ;;
        patch) NEW="${MAJOR}.${MINOR}.$((PATCH + 1))" ;;
        *) echo "unknown bump kind: {{bump}}"; exit 1 ;;
    esac
    # The flake reads the version from ./VERSION (baked in via ldflags),
    # so the bump lives there. sync-flake re-validates the build.
    echo "${NEW}" > VERSION
    just sync-flake --force
    if [ -n "$(git status --porcelain VERSION flake.nix)" ]; then
        git add VERSION flake.nix
        git commit -m "chore: bump to v${NEW}"
    fi
    git tag -a "v${NEW}" -m "v${NEW}"
    git push origin HEAD
    git push origin "v${NEW}"
    echo
    echo "Tagged v${NEW}. Watch the release build with: gh run watch"

release-patch: (_release "patch")
release-minor: (_release "minor")
release-major: (_release "major")
