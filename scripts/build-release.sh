#!/usr/bin/env bash
# Build cross-platform release archives from the current source tree.
set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "Usage: scripts/build-release.sh VERSION OUTPUT_DIR" >&2
  exit 2
fi

version="$1"
output_dir="$2"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "Version must use the stable X.Y.Z format." >&2
  exit 2
fi
source_version="$(sed -nE 's/^[[:space:]]*const Version[[:space:]]*=[[:space:]]*"([^"]+)".*/\1/p' "$root/internal/version/version.go")"
if [ "$source_version" != "$version" ]; then
  echo "Source version $source_version does not match release version $version." >&2
  exit 2
fi
if [ -e "$output_dir" ]; then
  echo "Output path already exists: $output_dir" >&2
  exit 2
fi

for command in go gzip tar zip; do
  command -v "$command" >/dev/null
done

mkdir -p "$(dirname "$output_dir")"
mkdir "$output_dir"
output_dir="$(cd "$output_dir" && pwd)"
staging_root="$(mktemp -d "${TMPDIR:-/tmp}/grand-lyon-mcp-release.XXXXXX")"
trap 'find "$staging_root" -depth -delete' EXIT
export COPYFILE_DISABLE=1
export SOURCE_DATE_EPOCH=315532800
export TZ=UTC

targets=(
  "linux/amd64/tar.gz"
  "linux/arm64/tar.gz"
  "darwin/amd64/tar.gz"
  "darwin/arm64/tar.gz"
  "windows/amd64/zip"
)
notice_args=("-output" "$staging_root/THIRD_PARTY_NOTICES.txt")
for target in "${targets[@]}"; do
  IFS=/ read -r goos goarch _ <<<"$target"
  notice_args+=("-target" "$goos/$goarch")
done
(
  cd "$root"
  CGO_ENABLED=0 go run ./scripts/notices "${notice_args[@]}"
)
archives=()

for target in "${targets[@]}"; do
  IFS=/ read -r goos goarch archive_format <<<"$target"
  package="grand-lyon-mcp_${version}_${goos}_${goarch}"
  package_dir="$staging_root/$package"
  mkdir "$package_dir"
  binary_name="grand-lyon-mcp"
  if [ "$goos" = "windows" ]; then
    binary_name="grand-lyon-mcp.exe"
  fi

  (
    cd "$root"
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags="-s -w" \
      -o "$package_dir/$binary_name" ./cmd/grand-lyon-mcp
  )
  cp "$root/ATTRIBUTIONS.md" "$root/LICENSE" "$root/README.md" \
    "$staging_root/THIRD_PARTY_NOTICES.txt" "$package_dir/"
  chmod 0755 "$package_dir/$binary_name"
  chmod 0644 "$package_dir/ATTRIBUTIONS.md" "$package_dir/LICENSE" \
    "$package_dir/README.md" "$package_dir/THIRD_PARTY_NOTICES.txt"
  find "$package_dir" -exec touch -t 198001010000.00 {} +

  archive="$package.$archive_format"
  if [ "$archive_format" = "zip" ]; then
    (cd "$staging_root" && zip -q -X -r "$output_dir/$archive" "$package")
  elif tar --version 2>/dev/null | grep -q GNU; then
    (
      cd "$staging_root"
      tar --sort=name --owner=0 --group=0 --numeric-owner \
        --mtime="@$SOURCE_DATE_EPOCH" -cf - "$package" | gzip -n >"$output_dir/$archive"
    )
  else
    (cd "$staging_root" && tar -cf - "$package" | gzip -n >"$output_dir/$archive")
  fi
  archives+=("$archive")
  find "$package_dir" -depth -delete
done

(
  cd "$output_dir"
  if command -v sha256sum >/dev/null; then
    sha256sum "${archives[@]}" >SHA256SUMS
  else
    shasum -a 256 "${archives[@]}" >SHA256SUMS
  fi
)

printf 'Built release assets in %s\n' "$output_dir"
