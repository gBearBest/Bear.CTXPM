#!/bin/sh
set -eu

OWNER="${CTXPM_INSTALL_OWNER:-gBearBest}"
REPO="${CTXPM_INSTALL_REPO:-Bear.CTXPM}"
BASE_URL_OVERRIDE="${CTXPM_INSTALL_BASE_URL:-}"
LATEST_TAG_OVERRIDE="${CTXPM_INSTALL_LATEST_TAG:-}"
INSTALL_SCOPE="global"
PROJECT_ROOT="."
INSTALL_DIR="${HOME}/.local/bin"
BIN_NAME="ctxpm"
VERSION="main"
USER_SET_BIN_NAME="false"

usage() {
  cat <<'EOF'
Usage: install.sh [options]

Options:
  --version <version>      Optional. Install main, latest, or a specific version such as v0.1.0 (default: main)
  --scope <global|project> Install as a global command or into a project's .ctxpm skill directory (default: global)
  --project-root <path>    Optional. Project root used when --scope project (default: current directory)
  --install-dir <path>     Install directory (default: ~/.local/bin)
  --bin-name <name>        Installed binary name (default: ctxpm; ctxpm.exe for Windows global installs)
  --no-modify-path         Reserved for future use; currently only prints PATH guidance
  -h, --help               Show this help
EOF
}

while [ $# -gt 0 ]; do
  case "$1" in
    --version)
      VERSION="$2"
      shift 2
      ;;
    --scope)
      INSTALL_SCOPE="$2"
      shift 2
      ;;
    --project-root)
      PROJECT_ROOT="$2"
      shift 2
      ;;
    --install-dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --bin-name)
      BIN_NAME="$2"
      USER_SET_BIN_NAME="true"
      shift 2
      ;;
    --no-modify-path)
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Required command not found: $1" >&2
    exit 1
  }
}

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "Neither sha256sum nor shasum is available." >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *)
      echo "Unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "Unsupported architecture: $(uname -m)" >&2
      exit 1
      ;;
  esac
}

resolve_version() {
  if [ "$VERSION" = "main" ]; then
    echo "main"
    return
  fi

  if [ "$VERSION" != "latest" ]; then
    echo "$VERSION"
    return
  fi

  if [ -n "$LATEST_TAG_OVERRIDE" ]; then
    echo "$LATEST_TAG_OVERRIDE"
    return
  fi

  need_cmd sed
  curl -fsSL \
    -H "Accept: application/vnd.github+json" \
    "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" \
    | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
    | head -n 1
}

resolve_asset_version() {
  case "$1" in
    main) echo "main" ;;
    v*) echo "${1#v}" ;;
    *) echo "$1" ;;
  esac
}

resolve_base_url() {
  if [ -n "$BASE_URL_OVERRIDE" ]; then
    echo "$BASE_URL_OVERRIDE"
    return
  fi

  printf 'https://github.com/%s/%s/releases/download/%s\n' "$OWNER" "$REPO" "$1"
}

need_cmd uname
need_cmd curl
need_cmd mktemp
need_cmd awk
need_cmd cp
need_cmd chmod
need_cmd mkdir
need_cmd mv
need_cmd rm

case "$INSTALL_SCOPE" in
  global|project)
    ;;
  *)
    echo "Unsupported install scope: $INSTALL_SCOPE" >&2
    exit 1
    ;;
esac

OS="$(detect_os)"
ARCH="$(detect_arch)"
TAG="$(resolve_version)"

ARCHIVE_EXT="tar.gz"
ARCHIVE_BINARY_NAME="ctxpm"
if [ "$OS" = "windows" ]; then
  ARCHIVE_EXT="zip"
  ARCHIVE_BINARY_NAME="ctxpm.exe"
  if [ "$INSTALL_SCOPE" = "global" ] && [ "$USER_SET_BIN_NAME" = "false" ]; then
    BIN_NAME="ctxpm.exe"
  fi
fi

if [ "$INSTALL_SCOPE" = "project" ]; then
  if [ "$BIN_NAME" != "ctxpm" ]; then
    echo "--bin-name is not supported with --scope project; the canonical project-local binary name is ctxpm." >&2
    exit 1
  fi
  INSTALL_DIR="${PROJECT_ROOT%/}/.ctxpm/dependencies/skills/ctxpm/cli"
fi

if [ "$ARCHIVE_EXT" = "zip" ]; then
  need_cmd unzip
else
  need_cmd tar
fi

if [ -z "$TAG" ]; then
  echo "Failed to resolve release version." >&2
  exit 1
fi

ASSET_VERSION="$(resolve_asset_version "$TAG")"
ASSET="ctxpm_${ASSET_VERSION}_${OS}_${ARCH}.${ARCHIVE_EXT}"
BASE_URL="$(resolve_base_url "$TAG")"
TMPDIR_ROOT="${TMPDIR:-/tmp}"
WORKDIR="$(mktemp -d "${TMPDIR_ROOT%/}/ctxpm-install-XXXXXX")"
ARCHIVE_PATH="${WORKDIR}/${ASSET}"
CHECKSUMS_PATH="${WORKDIR}/checksums.txt"
EXTRACT_DIR="${WORKDIR}/extract"
INSTALL_PATH="${INSTALL_DIR}/${BIN_NAME}"
TEMP_INSTALL_PATH="${INSTALL_PATH}.tmp.$$"
PROJECT_EXE_PATH=""
PROJECT_CMD_PATH=""
TEMP_PROJECT_EXE_PATH=""
TEMP_PROJECT_CMD_PATH=""

write_windows_project_shim() {
  cat > "$1" <<'EOF'
#!/bin/sh
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
exec "${SCRIPT_DIR}/ctxpm.exe" "$@"
EOF
}

write_windows_project_cmd_shim() {
  cat > "$1" <<'EOF'
@echo off
"%~dp0ctxpm.exe" %*
EOF
}

cleanup() {
  rm -rf "$WORKDIR"
}
trap cleanup EXIT INT TERM

mkdir -p "$EXTRACT_DIR"
mkdir -p "$INSTALL_DIR"

echo "Installing ctxpm ${TAG} for ${OS}/${ARCH} (${INSTALL_SCOPE})..."
curl -fsSL "${BASE_URL}/${ASSET}" -o "$ARCHIVE_PATH"
curl -fsSL "${BASE_URL}/checksums.txt" -o "$CHECKSUMS_PATH"

EXPECTED_SUM="$(awk -v file="$ASSET" '$2 == file {print $1}' "$CHECKSUMS_PATH")"
if [ -z "$EXPECTED_SUM" ]; then
  echo "Could not find checksum entry for ${ASSET}." >&2
  exit 1
fi

ACTUAL_SUM="$(sha256_file "$ARCHIVE_PATH")"
if [ "$EXPECTED_SUM" != "$ACTUAL_SUM" ]; then
  echo "Checksum mismatch for ${ASSET}." >&2
  echo "Expected: $EXPECTED_SUM" >&2
  echo "Actual:   $ACTUAL_SUM" >&2
  exit 1
fi

if [ "$ARCHIVE_EXT" = "zip" ]; then
  unzip -q "$ARCHIVE_PATH" -d "$EXTRACT_DIR"
else
  tar -xzf "$ARCHIVE_PATH" -C "$EXTRACT_DIR"
fi

if [ ! -f "${EXTRACT_DIR}/${ARCHIVE_BINARY_NAME}" ]; then
  echo "Archive did not contain expected binary: ${ARCHIVE_BINARY_NAME}" >&2
  exit 1
fi

if [ "$INSTALL_SCOPE" = "project" ] && [ "$OS" = "windows" ]; then
  PROJECT_EXE_PATH="${INSTALL_DIR}/ctxpm.exe"
  PROJECT_CMD_PATH="${INSTALL_DIR}/ctxpm.cmd"
  TEMP_PROJECT_EXE_PATH="${PROJECT_EXE_PATH}.tmp.$$"
  TEMP_PROJECT_CMD_PATH="${PROJECT_CMD_PATH}.tmp.$$"

  cp "${EXTRACT_DIR}/${ARCHIVE_BINARY_NAME}" "$TEMP_PROJECT_EXE_PATH"
  chmod 755 "$TEMP_PROJECT_EXE_PATH"
  write_windows_project_shim "$TEMP_INSTALL_PATH"
  write_windows_project_cmd_shim "$TEMP_PROJECT_CMD_PATH"
  chmod 755 "$TEMP_INSTALL_PATH" "$TEMP_PROJECT_CMD_PATH"

  mv "$TEMP_PROJECT_EXE_PATH" "$PROJECT_EXE_PATH"
  mv "$TEMP_PROJECT_CMD_PATH" "$PROJECT_CMD_PATH"
  mv "$TEMP_INSTALL_PATH" "$INSTALL_PATH"
else
  cp "${EXTRACT_DIR}/${ARCHIVE_BINARY_NAME}" "$TEMP_INSTALL_PATH"
  chmod 755 "$TEMP_INSTALL_PATH"
  mv "$TEMP_INSTALL_PATH" "$INSTALL_PATH"
fi

echo "Installed to: $INSTALL_PATH"
echo "Run: ${INSTALL_PATH} --help"

if [ "$INSTALL_SCOPE" = "project" ] && [ "$OS" = "windows" ]; then
  echo "Also installed: ${INSTALL_DIR}/ctxpm.exe"
  echo "Also installed: ${INSTALL_DIR}/ctxpm.cmd"
fi

if [ "$INSTALL_SCOPE" = "global" ]; then
  case ":${PATH}:" in
    *:"${INSTALL_DIR}":*)
      ;;
    *)
      echo
      echo "Note: ${INSTALL_DIR} is not currently on your PATH."
      echo "Add it to your shell profile if you want to run '${BIN_NAME}' directly."
      ;;
  esac
fi
