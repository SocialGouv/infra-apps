#!/bin/sh
# Sourced by both bootstrap readers. Configuration comes from pod env, not
# interpolated shell. A Sentinel answer is a host/port pair, never just a host.

valkeyha_cli() {
    # The CLI's -t bounds connection only. Bound the complete command here.
    # Do not use -e: RESP errors must remain on stdout so they cannot look
    # like an unreachable Sentinel and accidentally authorize bootstrap.
    if [ "$VALKEY_DISCOVERY_COMMAND_TIMEOUT" -eq 0 ]; then
        valkey-cli --raw "$@"
    else
        timeout -s KILL "$VALKEY_DISCOVERY_COMMAND_TIMEOUT" valkey-cli --raw "$@"
    fi
}

valkeyha_is_self() {
    [ "$2" = "$VALKEY_DISCOVERY_DATA_PORT" ] || return 1
    case "$1" in
        "$discovery_self"|"$discovery_self.$VALKEY_DISCOVERY_HEADLESS"|"$discovery_self.$VALKEY_DISCOVERY_FULL_HEADLESS") return 0 ;;
    esac
    for discovery_ip in $discovery_ips; do
        [ "$1" = "$discovery_ip" ] && return 0
    done
    return 1
}

valkeyha_role_is_master() {
    discovery_role=$(valkeyha_cli -h "$1" -p "$2" ROLE 2>/dev/null) || return 1
    [ "${discovery_role%%"$discovery_newline"*}" = master ]
}

valkeyha_note_seen() {
    # This emptyDir survives init/container retries in the same pod. Once
    # observed, a master (or an explicit invalid reply) cannot become a
    # fresh-cluster premise merely because the next query is silent.
    : > "$VALKEY_DISCOVERY_STATE_FILE" || return 1
    discovery_seen=true
}

valkeyha_select_master() {
    discovery_mode=$1
    command -v valkey-cli >/dev/null || return 1
    if [ "$VALKEY_DISCOVERY_COMMAND_TIMEOUT" -ne 0 ]; then
        command -v timeout >/dev/null || return 1
    fi
    discovery_self=$(hostname)
    discovery_ips=$(hostname -i)
    discovery_newline='
'
    discovery_seen=false
    [ ! -e "$VALKEY_DISCOVERY_STATE_FILE" ] || discovery_seen=true
    discovery_attempt=0
    if [ "$VALKEY_DISCOVERY_AUTH" = true ]; then
        export VALKEYCLI_AUTH="$VALKEY_PASSWORD"
    fi

    while :; do
        discovery_attempt=$((discovery_attempt + 1))
        discovery_self_host=
        discovery_self_port=
        discovery_index=0
        while [ "$discovery_index" -lt "$VALKEY_DISCOVERY_REPLICAS" ]; do
            discovery_peer="$VALKEY_DISCOVERY_PREFIX-$discovery_index.$VALKEY_DISCOVERY_HEADLESS"
            discovery_index=$((discovery_index + 1))
            [ "$discovery_peer" != "$discovery_self.$VALKEY_DISCOVERY_HEADLESS" ] || continue
            discovery_reply=$(valkeyha_cli -h "$discovery_peer" -p "$VALKEY_DISCOVERY_SENTINEL_PORT" \
                SENTINEL get-master-addr-by-name "$VALKEY_DISCOVERY_MASTER_NAME" 2>/dev/null) || :
            [ -n "$discovery_reply" ] || continue
            valkeyha_note_seen || return 1
            case "$discovery_reply" in
                *"$discovery_newline"*)
                    discovery_host=${discovery_reply%%"$discovery_newline"*}
                    discovery_port=${discovery_reply#*"$discovery_newline"}
                    ;;
                *) echo "Sentinel discovery: expected a host/port pair from $discovery_peer" >&2; continue ;;
            esac
            case "$discovery_host" in
                ''|*[[:space:]]*) echo "Sentinel discovery: invalid host from $discovery_peer" >&2; continue ;;
            esac
            case "$discovery_port" in
                ''|*[!0-9]*) echo "Sentinel discovery: invalid port from $discovery_peer" >&2; continue ;;
            esac
            if [ "${#discovery_port}" -gt 5 ] || [ "$discovery_port" -lt 1 ] || [ "$discovery_port" -gt 65535 ]; then
                echo "Sentinel discovery: port outside 1..65535 from $discovery_peer" >&2
                continue
            fi
            if valkeyha_is_self "$discovery_host" "$discovery_port"; then
                # The init reader's server cannot answer ROLE yet. Keep this
                # recovery candidate, but prefer any verified distant master.
                if [ "$discovery_mode" = init ] || valkeyha_role_is_master "$discovery_host" "$discovery_port"; then
                    discovery_self_host=$discovery_host
                    discovery_self_port=$discovery_port
                fi
                continue
            fi
            if valkeyha_role_is_master "$discovery_host" "$discovery_port"; then
                DISCOVERED_MASTER_HOST=$discovery_host
                DISCOVERED_MASTER_PORT=$discovery_port
                return 0
            fi
        done

        if [ -n "$discovery_self_host" ]; then
            DISCOVERED_MASTER_HOST=$discovery_self_host
            DISCOVERED_MASTER_PORT=$discovery_self_port
            return 0
        fi
        if [ "$discovery_seen" = false ]; then
            DISCOVERED_MASTER_HOST="$VALKEY_DISCOVERY_PREFIX-0.$VALKEY_DISCOVERY_FULL_HEADLESS"
            DISCOVERED_MASTER_PORT=$VALKEY_DISCOVERY_DATA_PORT
            # Preserve first installation: ordinal zero can start before a
            # Sentinel exists. A full outage with no observable Sentinel is
            # indistinguishable here and remains an operator recovery case.
            if [ "$discovery_mode" = init ] || valkeyha_role_is_master "$DISCOVERED_MASTER_HOST" "$DISCOVERED_MASTER_PORT"; then
                return 0
            fi
        fi
        if [ "$VALKEY_DISCOVERY_MAX_ATTEMPTS" -gt 0 ] && [ "$discovery_attempt" -ge "$VALKEY_DISCOVERY_MAX_ATTEMPTS" ]; then
            echo "Sentinel discovery: no verified master after $discovery_attempt attempts; refusing bootstrap" >&2
            return 1
        fi
        echo "Sentinel discovery: waiting for a reachable master ($discovery_attempt)" >&2
        sleep "$VALKEY_DISCOVERY_RETRY_SECONDS"
    done
}
