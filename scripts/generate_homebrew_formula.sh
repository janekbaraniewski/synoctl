#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  generate_homebrew_formula.sh \
    --formula-name opencontext \
    --formula-class Opencontext \
    --binary octx \
    --desc "..." \
    --homepage "https://github.com/owner/repo" \
    --version "1.2.3" \
    --repo "owner/repo" \
    --license "MIT" \
    --sha-darwin-amd64 <sha> \
    --sha-darwin-arm64 <sha> \
    --sha-linux-amd64 <sha> \
    --sha-linux-arm64 <sha> \
    --output /path/to/formula.rb
EOF
}

formula_name=""
formula_class=""
binary=""
desc=""
homepage=""
version=""
repo=""
license_name="MIT"
sha_darwin_amd64=""
sha_darwin_arm64=""
sha_linux_amd64=""
sha_linux_arm64=""
output=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --formula-name)
      formula_name="$2"
      shift 2
      ;;
    --formula-class)
      formula_class="$2"
      shift 2
      ;;
    --binary)
      binary="$2"
      shift 2
      ;;
    --desc)
      desc="$2"
      shift 2
      ;;
    --homepage)
      homepage="$2"
      shift 2
      ;;
    --version)
      version="$2"
      shift 2
      ;;
    --repo)
      repo="$2"
      shift 2
      ;;
    --license)
      license_name="$2"
      shift 2
      ;;
    --sha-darwin-amd64)
      sha_darwin_amd64="$2"
      shift 2
      ;;
    --sha-darwin-arm64)
      sha_darwin_arm64="$2"
      shift 2
      ;;
    --sha-linux-amd64)
      sha_linux_amd64="$2"
      shift 2
      ;;
    --sha-linux-arm64)
      sha_linux_arm64="$2"
      shift 2
      ;;
    --output)
      output="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

for value in \
  formula_name \
  formula_class \
  binary \
  desc \
  homepage \
  version \
  repo \
  sha_darwin_amd64 \
  sha_darwin_arm64 \
  sha_linux_amd64 \
  sha_linux_arm64 \
  output
do
  if [[ -z "${!value}" ]]; then
    echo "missing required argument: ${value}" >&2
    usage >&2
    exit 1
  fi
done

base_url="https://github.com/${repo}/releases/download/v${version}"
mkdir -p "$(dirname "$output")"

cat >"$output" <<EOF
# typed: false
# frozen_string_literal: true

class ${formula_class} < Formula
  desc "${desc}"
  homepage "${homepage}"
  version "${version}"
  license "${license_name}"

  on_macos do
    if Hardware::CPU.arm?
      url "${base_url}/${formula_name}_${version}_darwin_arm64.tar.gz"
      sha256 "${sha_darwin_arm64}"
    else
      url "${base_url}/${formula_name}_${version}_darwin_amd64.tar.gz"
      sha256 "${sha_darwin_amd64}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${base_url}/${formula_name}_${version}_linux_arm64.tar.gz"
      sha256 "${sha_linux_arm64}"
    else
      url "${base_url}/${formula_name}_${version}_linux_amd64.tar.gz"
      sha256 "${sha_linux_amd64}"
    end
  end

  def install
    bin.install "${binary}"
  end

  test do
    assert_match "${binary}", shell_output("#{bin}/${binary} version 2>&1", 0)
  end
end
EOF
