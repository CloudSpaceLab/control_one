#!/usr/bin/env bash
# Rollback for CIS 5.2.10 — restore prior password auth settings
set -euo pipefail

CONFIG="/etc/ssh/sshd_config"
BACKUP="${CONFIG}.controlone.pwauth.bak"

if [[ -f "$BACKUP" ]]; then
    cp -p "$BACKUP" "$CONFIG"
    rm -f "$BACKUP"
else
    sed -i -E '/^\s*#?\s*PasswordAuthentication\s+/d' "$CONFIG"
    sed -i -E '/^\s*#?\s*ChallengeResponseAuthentication\s+/d' "$CONFIG"
    echo "PasswordAuthentication yes" >> "$CONFIG"
fi

sshd -t
systemctl reload sshd 2>/dev/null || systemctl reload ssh 2>/dev/null || service ssh reload
echo "PasswordAuthentication restored"
