#!/usr/bin/env bash
# Create a 1 GB swapfile on /swapfile if one isn't already active.
# Idempotent: safe to re-run.
#
# Run once during droplet provisioning, before `docker compose up`.
# See docs/specs/observability/design.md "512 MB mitigations".

set -euo pipefail

SWAPFILE=/swapfile
SIZE_MB=1024

if swapon --show=NAME --noheadings | grep -qx "${SWAPFILE}"; then
    echo "swap: ${SWAPFILE} already active — nothing to do"
    exit 0
fi

if [ -f "${SWAPFILE}" ]; then
    echo "swap: ${SWAPFILE} exists but isn't active; activating"
else
    echo "swap: creating ${SWAPFILE} (${SIZE_MB} MB)"
    fallocate -l "${SIZE_MB}M" "${SWAPFILE}" || \
        dd if=/dev/zero of="${SWAPFILE}" bs=1M count="${SIZE_MB}"
    chmod 600 "${SWAPFILE}"
    mkswap "${SWAPFILE}"
fi

swapon "${SWAPFILE}"

# Persist across reboots.
if ! grep -qE "^${SWAPFILE}\\s+none\\s+swap" /etc/fstab; then
    echo "${SWAPFILE} none swap sw 0 0" >> /etc/fstab
fi

# Conservative swappiness: only swap under real memory pressure.
sysctl -w vm.swappiness=10 >/dev/null
if ! grep -q '^vm.swappiness' /etc/sysctl.d/99-swappiness.conf 2>/dev/null; then
    echo "vm.swappiness=10" > /etc/sysctl.d/99-swappiness.conf
fi

echo "swap: done"
swapon --show
