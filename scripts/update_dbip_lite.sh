#!/usr/bin/env bash
set -euo pipefail

OUT_DIR="${1:-/opt/control-one/deploy/ip-intel}"
ACCEPTED="${DBIP_LICENSE_ACCEPTED:-0}"
GROUP_ID="${IP_INTEL_GID:-65532}"
MAX_AGE_DAYS="${DBIP_MAX_AGE_DAYS:-20}"

if [[ "$ACCEPTED" != "1" ]]; then
  echo "DB-IP Lite is CC BY 4.0. Set DBIP_LICENSE_ACCEPTED=1 after accepting the license." >&2
  exit 2
fi

command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 2; }
command -v gzip >/dev/null 2>&1 || { echo "gzip is required" >&2; exit 2; }

mkdir -p "$OUT_DIR"

is_fresh() {
  local file="$1"
  [[ -s "$file" ]] && find "$file" -mtime "-$MAX_AGE_DAYS" -print -quit 2>/dev/null | grep -q .
}

month_candidates() {
  date -u +%Y-%m
  date -u -d '1 month ago' +%Y-%m
}

download_one() {
  local kind="$1"
  local target="$2"

  if is_fresh "$target"; then
    echo "$kind MMDB is fresh: $target"
    return 0
  fi

  local month url tmp_gz tmp_db
  tmp_gz="${target}.gz.new.$$"
  tmp_db="${target}.new.$$"
  trap 'rm -f "$tmp_gz" "$tmp_db"' RETURN

  while read -r month; do
    [[ -n "$month" ]] || continue
    url="https://download.db-ip.com/free/dbip-${kind}-lite-${month}.mmdb.gz"
    echo "Fetching $url"
    if curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 10 --max-time 300 "$url" -o "$tmp_gz"; then
      gzip -t "$tmp_gz"
      gzip -dc "$tmp_gz" > "$tmp_db"
      if [[ ! -s "$tmp_db" ]] || [[ "$(wc -c < "$tmp_db")" -lt 1048576 ]]; then
        echo "Downloaded $kind MMDB is unexpectedly small" >&2
        rm -f "$tmp_gz" "$tmp_db"
        continue
      fi
      chmod 0640 "$tmp_db"
      chown "0:$GROUP_ID" "$tmp_db" 2>/dev/null || chgrp "$GROUP_ID" "$tmp_db" 2>/dev/null || true
      mv -f "$tmp_db" "$target"
      rm -f "$tmp_gz"
      printf '%s\n' "$month" > "${target}.version"
      chmod 0640 "${target}.version"
      chown "0:$GROUP_ID" "${target}.version" 2>/dev/null || chgrp "$GROUP_ID" "${target}.version" 2>/dev/null || true
      echo "Installed $kind MMDB release $month"
      trap - RETURN
      return 0
    fi
    rm -f "$tmp_gz" "$tmp_db"
  done < <(month_candidates)

  echo "Unable to refresh DB-IP $kind Lite MMDB" >&2
  return 1
}

city_ok=0
asn_ok=0
download_one city "$OUT_DIR/dbip-city-lite.mmdb" || city_ok=$?
download_one asn "$OUT_DIR/dbip-asn-lite.mmdb" || asn_ok=$?

cat > "$OUT_DIR/NOTICE.txt" <<'EOF'
DB-IP Lite databases are licensed under Creative Commons Attribution 4.0.
IP Geolocation by DB-IP: https://db-ip.com
EOF
chmod 0640 "$OUT_DIR/NOTICE.txt"
chown "0:$GROUP_ID" "$OUT_DIR/NOTICE.txt" 2>/dev/null || chgrp "$GROUP_ID" "$OUT_DIR/NOTICE.txt" 2>/dev/null || true

if (( city_ok != 0 || asn_ok != 0 )); then
  exit 1
fi
