#!/usr/bin/env bash
# Signed OVH API v1 calls for Managed Kubernetes control plane operations.
#
# Usage:
#   ovh-kube-restart.sh names                # map kubeIds -> name/region/status
#   ovh-kube-restart.sh status <kubeId>      # one cluster: name, region, version, status
#   ovh-kube-restart.sh restart <kubeId> [force]   # force: true|false (default false)
#   ovh-kube-restart.sh watch <kubeId>       # poll status until READY
#
# Credentials (never printed by this script): OVH_AK, OVH_AS, OVH_CK in the
# environment, or in the file pointed to by OVH_ENV_FILE
# (default: ~/lab/fabrique/.secrets/ovh-api.env).
#
# Project serviceName can be overridden with OVH_SERVICE.
set -euo pipefail

SERVICE="${OVH_SERVICE:-324650435c1a4ad59048560bd40bc88f}"
API="https://eu.api.ovh.com/1.0"
ENV_FILE="${OVH_ENV_FILE:-$HOME/lab/fabrique/.secrets/ovh-api.env}"

if [[ -z "${OVH_AK:-}" || -z "${OVH_AS:-}" || -z "${OVH_CK:-}" ]]; then
  # shellcheck disable=SC1090
  source "$ENV_FILE"
fi
: "${OVH_AK:?OVH_AK not set}" "${OVH_AS:?OVH_AS not set}" "${OVH_CK:?OVH_CK not set}"

# OVH signature: $1$ + sha1hex(app_secret + "+" + consumer_key + "+" + method
#                + "+" + query_url + "+" + body + "+" + timestamp)
call() {
  local method="$1" path="$2" body="${3:-}"
  local url="$API$path"
  local ts
  ts=$(curl -s "$API/auth/time")
  local sig
  sig="\$1\$$(printf '%s' "$OVH_AS+$OVH_CK+$method+$url+$body+$ts" | sha1sum | cut -d' ' -f1)"
  curl -sS -X "$method" "$url" \
    -H "Content-Type: application/json" \
    -H "X-Ovh-Application: $OVH_AK" \
    -H "X-Ovh-Consumer: $OVH_CK" \
    -H "X-Ovh-Timestamp: $ts" \
    -H "X-Ovh-Signature: $sig" \
    ${body:+-d "$body"}
}

cmd="${1:?usage: ovh-kube-restart.sh names|status|restart|watch [args]}"
case "$cmd" in
  names)
    for id in $(call GET "/cloud/project/$SERVICE/kube" | tr -d '[]" ' | tr ',' '\n'); do
      call GET "/cloud/project/$SERVICE/kube/$id" \
        | python3 -c "import json,sys; d=json.load(sys.stdin); print(d['id'], d['name'], d['region'], d['version'], d['status'])"
    done
    ;;
  status)
    kube="${2:?kubeId missing}"
    call GET "/cloud/project/$SERVICE/kube/$kube" \
      | python3 -c "import json,sys; d=json.load(sys.stdin); print(json.dumps(d, indent=1))"
    ;;
  restart)
    kube="${2:?kubeId missing}"
    force="${3:-false}"
    call POST "/cloud/project/$SERVICE/kube/$kube/restart" "{\"force\": $force}"
    echo
    ;;
  watch)
    kube="${2:?kubeId missing}"
    while :; do
      status=$(call GET "/cloud/project/$SERVICE/kube/$kube" \
        | python3 -c "import json,sys; print(json.load(sys.stdin)['status'])")
      echo "$(date -u +%H:%M:%S) $status"
      [[ "$status" == "READY" ]] && exit 0
      sleep 15
    done
    ;;
  *)
    echo "unknown command: $cmd" >&2
    exit 1
    ;;
esac
