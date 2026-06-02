{{- define "talos.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "talos.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "talos.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "talos.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "talos.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: forge
{{- end -}}

{{- define "talos.selectorLabels" -}}
app.kubernetes.io/name: {{ include "talos.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "talos.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "talos.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "talos.image" -}}
{{- printf "%s:%s" .Values.image.repository (.Values.image.tag | default .Chart.AppVersion) -}}
{{- end -}}

{{- define "talos.dbSecretName" -}}
{{- if .Values.database.existingSecret -}}
{{- .Values.database.existingSecret -}}
{{- else -}}
{{- printf "%s-db" (include "talos.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/* Shared env block for server + migrator. */}}
{{- define "talos.env" -}}
- name: SVC_NAME
  value: {{ include "talos.name" . | quote }}
- name: REST_ADDRESS
  value: ":{{ .Values.ports.http }}"
- name: HTTP_ADDRESS
  value: ":{{ .Values.ports.http }}"
- name: GRPC_ADDRESS
  value: ":{{ .Values.ports.grpc }}"
- name: DB_HOST
  value: {{ .Values.database.host | quote }}
- name: DB_PORT
  value: {{ .Values.database.port | quote }}
- name: DB_NAME
  value: {{ .Values.database.name | quote }}
- name: DB_SCHEMA
  value: {{ .Values.database.schema | quote }}
- name: DB_SSL
  value: {{ .Values.database.ssl | quote }}
- name: DB_LOG_LEVEL
  value: {{ .Values.database.logLevel | default "warn" | quote }}
- name: DB_USER
  valueFrom:
    secretKeyRef:
      name: {{ include "talos.dbSecretName" . }}
      key: DB_USER
- name: DB_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "talos.dbSecretName" . }}
      key: DB_PASSWORD
- name: TALOS_RETENTION_MONTHS
  value: {{ .Values.retentionMonths | default 6 | quote }}
{{- if .Values.streamHmacSecret }}
- name: TALOS_STREAM_HMAC_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "talos.fullname" . }}-stream
      key: TALOS_STREAM_HMAC_SECRET
{{- end }}
{{- with .Values.wsAllowedOrigins }}
- name: TALOS_WS_ALLOWED_ORIGINS
  value: {{ . | quote }}
{{- end }}
{{- if .Values.wsAllowInsecure }}
# Opt out of secure-by-default WS auth (no HMAC secret + allow all origins).
# Intended for local/dev only; prod must set streamHmacSecret instead.
- name: TALOS_WS_ALLOW_INSECURE
  value: "1"
{{- end }}
{{- if .Values.gatewaySecret }}
- name: FORGE_GATEWAY_SECRET
  value: {{ .Values.gatewaySecret | quote }}
{{- end }}
{{- end -}}
