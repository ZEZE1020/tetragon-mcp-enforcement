#!/usr/bin/env bash
set -euo pipefail

REGISTRY="ghcr.io"
OWNER="zeze1020"
IMAGE_NAME="mcp-gateway"
IMAGE="${REGISTRY}/${OWNER}/${IMAGE_NAME}:latest"
CONTEXT_DIR="assets/gateway"
PLATFORMS_LOCAL="linux/amd64"

# Resolve token
if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  if command -v gh >/dev/null 2>&1; then
    GITHUB_TOKEN="$(gh auth token 2>/dev/null || true)"
  fi
fi

if [[ -z "${GITHUB_TOKEN:-}" ]]; then
  echo "Error: GITHUB_TOKEN is not set and 'gh auth token' did not return a token." >&2
  echo "Set GITHUB_TOKEN (with packages:write scope) or run 'gh auth login'." >&2
  exit 1
fi

echo "Logging in to ${REGISTRY} as ZEZE1020..."
echo "${GITHUB_TOKEN}" | docker login "${REGISTRY}" -u ZEZE1020 --password-stdin

# Determine platforms (amd64 required for Killercoda; add arm64 if buildx available)
PLATFORMS="${PLATFORMS_LOCAL}"
if docker buildx version >/dev/null 2>&1; then
  # Ensure a builder exists for multi-arch when possible
  if ! docker buildx inspect multiarch-builder >/dev/null 2>&1; then
    docker buildx create --name multiarch-builder --use >/dev/null 2>&1 || docker buildx use default >/dev/null 2>&1 || true
  else
    docker buildx use multiarch-builder >/dev/null 2>&1 || true
  fi
  PLATFORMS="linux/amd64,linux/arm64"
  echo "Building and pushing ${IMAGE} for ${PLATFORMS}..."
  docker buildx build \
    --platform "${PLATFORMS}" \
    -t "${IMAGE}" \
    -f "${CONTEXT_DIR}/Dockerfile" \
    --push \
    "${CONTEXT_DIR}"
else
  echo "docker buildx not available; building and pushing ${IMAGE} for ${PLATFORMS_LOCAL} only..."
  docker build --platform "${PLATFORMS_LOCAL}" -t "${IMAGE}" -f "${CONTEXT_DIR}/Dockerfile" "${CONTEXT_DIR}"
  docker push "${IMAGE}"
fi

echo "Pushed ${IMAGE}"
