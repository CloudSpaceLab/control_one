#!/usr/bin/env bash
# CIS 1.1.1.x — Disable unused filesystem kernel modules
# SOC2 CC6.6 — Reduce kernel attack surface
set -euo pipefail

MODULES=(cramfs freevxfs jffs2 hfs hfsplus udf squashfs)

BLACKLIST=/etc/modprobe.d/controlone-disabled-fs.conf
{
    echo "# Managed by ControlOne — CIS 1.1.1.x"
    for mod in "${MODULES[@]}"; do
        echo "install ${mod} /bin/true"
        echo "blacklist ${mod}"
    done
} > "$BLACKLIST"

# Unload any currently loaded instance so the policy takes effect immediately.
for mod in "${MODULES[@]}"; do
    if lsmod | awk '{print $1}' | grep -qx "$mod"; then
        modprobe -r "$mod" 2>/dev/null || true
    fi
done

# Rebuild initramfs on distros that honour modprobe drops there.
if command -v update-initramfs >/dev/null 2>&1; then
    update-initramfs -u
elif command -v dracut >/dev/null 2>&1; then
    dracut -f
fi

echo "Unused filesystem modules blacklisted"
