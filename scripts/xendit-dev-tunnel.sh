#!/usr/bin/env bash
set -euo pipefail

action="${1:-status}"
proxy_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
api_port="${API_PORT:-}"
if [[ -z "$api_port" ]]; then
  published_address="$(cd "$proxy_root" && docker compose port api 8080 2>/dev/null | head -1 || true)"
  api_port="${published_address##*:}"
fi
api_port="${api_port:-18080}"
state_dir="${TMPDIR:-/tmp}/emisell-payment-proxy-tunnel"
pid_file="${state_dir}/ngrok.pid"
log_file="${state_dir}/ngrok.log"
tunnel_name="emisell-payment-proxy"

existing_public_url() {
  curl -fsS http://127.0.0.1:4040/api/tunnels 2>/dev/null |
    jq -r --arg name "$tunnel_name" '.tunnels[]? | select(.name==$name and .proto=="https") | .public_url' |
    head -1
}

start_tunnel() {
  command -v ngrok >/dev/null || { echo "ngrok is required" >&2; exit 1; }
  command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
  command -v jq >/dev/null || { echo "jq is required" >&2; exit 1; }
  mkdir -p "$state_dir"
  public_url="$(existing_public_url || true)"
  if [[ -z "$public_url" ]] && curl -fsS http://127.0.0.1:4040/api/tunnels >/dev/null 2>&1; then
    public_url="$(curl -fsS -X POST http://127.0.0.1:4040/api/tunnels \
      -H 'Content-Type: application/json' \
      --data "{\"name\":\"$tunnel_name\",\"addr\":\"http://localhost:$api_port\",\"proto\":\"http\"}" |
      jq -r '.public_url // empty')"
  fi
  if [[ -z "$public_url" ]]; then
    ngrok http "$api_port" --log stdout --log-format json >"$log_file" 2>&1 &
    echo "$!" >"$pid_file"
  fi
  for _ in {1..30}; do
    public_url="$(existing_public_url || true)"
    if [[ -z "$public_url" ]] && [[ -f "$pid_file" ]] && kill -0 "$(<"$pid_file")" 2>/dev/null; then
      public_url="$(curl -fsS http://127.0.0.1:4040/api/tunnels 2>/dev/null |
        jq -r --arg addr "http://localhost:$api_port" '.tunnels[]? | select(.proto=="https" and .config.addr==$addr) | .public_url' |
        head -1 || true)"
    fi
    [[ -n "$public_url" ]] && break
    sleep 1
  done
  [[ -n "$public_url" ]] || { echo "ngrok did not expose an HTTPS URL" >&2; exit 1; }
  (cd "$proxy_root" && PAYMENT_PROXY_PUBLIC_BASE_URL="$public_url" docker compose up -d --no-deps --force-recreate api)
  echo "Payment Proxy public URL: $public_url"
  echo "Reconfigure the Xendit installation so direct webhook URLs are registered."
}

stop_tunnel() {
  curl -fsS -X DELETE "http://127.0.0.1:4040/api/tunnels/$tunnel_name" >/dev/null 2>&1 || true
  if [[ -f "$pid_file" ]]; then
    pid="$(<"$pid_file")"
    kill "$pid" 2>/dev/null || true
    rm -f "$pid_file"
  fi
  (cd "$proxy_root" && PAYMENT_PROXY_PUBLIC_BASE_URL= docker compose up -d --no-deps --force-recreate api)
  echo "Xendit development tunnel stopped"
}

case "$action" in
  start) start_tunnel ;;
  stop) stop_tunnel ;;
  status)
    if [[ -n "$(existing_public_url || true)" ]] || { [[ -f "$pid_file" ]] && kill -0 "$(<"$pid_file")" 2>/dev/null; }; then
      echo "Xendit development tunnel is running"
    else
      echo "Xendit development tunnel is stopped"
    fi
    ;;
  *) echo "usage: $0 [start|stop|status]" >&2; exit 2 ;;
esac
