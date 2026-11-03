{{/* Pod env is data: no operator value becomes interpolated shell source. */}}
{{- define "valkeyha.masterDiscoveryEnv" -}}
- name: VALKEY_DISCOVERY_PREFIX
  value: {{ include "valkey.fullname" . | quote }}
- name: VALKEY_DISCOVERY_HEADLESS
  value: {{ printf "%s-headless" (include "valkey.fullname" .) | quote }}
- name: VALKEY_DISCOVERY_FULL_HEADLESS
  value: {{ printf "%s-headless.%s.svc.%s" (include "valkey.fullname" .) .Release.Namespace .Values.clusterDomain | quote }}
- name: VALKEY_DISCOVERY_REPLICAS
  value: {{ .Values.replicaCount | quote }}
- name: VALKEY_DISCOVERY_SENTINEL_PORT
  value: {{ .Values.sentinel.port | quote }}
- name: VALKEY_DISCOVERY_DATA_PORT
  value: {{ .Values.service.port | quote }}
- name: VALKEY_DISCOVERY_MASTER_NAME
  value: {{ .Values.sentinel.masterName | quote }}
- name: VALKEY_DISCOVERY_AUTH
  value: {{ .Values.auth.enabled | quote }}
- name: VALKEY_DISCOVERY_MAX_ATTEMPTS
  value: {{ .Values.sentinel.masterDiscovery.maxAttempts | quote }}
- name: VALKEY_DISCOVERY_RETRY_SECONDS
  value: {{ .Values.sentinel.masterDiscovery.retrySeconds | quote }}
- name: VALKEY_DISCOVERY_COMMAND_TIMEOUT
  value: {{ .Values.sentinel.masterDiscovery.commandTimeoutSeconds | quote }}
- name: VALKEY_DISCOVERY_STATE_FILE
  value: /tmp/valkey/sentinel-seen
{{- end -}}
