#!/usr/bin/env bash
# Rollback — remove the blacklist drop-in; modules can load on demand again.
set -euo pipefail

BLACKLIST=/etc/modprobe.d/controlone-disabled-fs.conf
rm -f "$BLACKLIST"

if command -v update-initramfs >/dev/null 2>&1; then
    update-initramfs -u
elif command -v dracut >/dev/null 2>&1; then
    dracut -f
fi

echo "Unused-filesystem-modules blacklist removed"
