{{/* Chart name, overridable. */}}
{{- define "carillon.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Fully-qualified app name. */}}
{{- define "carillon.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/* Valkey resource name. */}}
{{- define "carillon.valkeyFullname" -}}
{{- printf "%s-valkey" (include "carillon.fullname" .) -}}
{{- end -}}

{{/* Common labels. */}}
{{- define "carillon.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "carillon.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: carillon
{{- end -}}

{{/* Selector labels. */}}
{{- define "carillon.selectorLabels" -}}
app.kubernetes.io/name: {{ include "carillon.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* Carillon image ref (tag defaults to appVersion). */}}
{{- define "carillon.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}

{{/* Name of the Secret to mount (created or external). */}}
{{- define "carillon.secretName" -}}
{{- if .Values.existingSecret -}}
{{- .Values.existingSecret -}}
{{- else -}}
{{- printf "%s-secrets" (include "carillon.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Whether a Secret should be mounted: an external one, or a chart-created one that
actually has files. Emits "true" or "" (falsey) for use in `if`.
*/}}
{{- define "carillon.hasSecret" -}}
{{- if or (and .Values.secrets.create .Values.secrets.files) .Values.existingSecret -}}
true
{{- end -}}
{{- end -}}

{{/*
CARILLON_REDIS_URL value: the bundled Valkey Service when enabled, otherwise the
operator-supplied externalRedisUrl ("" => stateless).
*/}}
{{- define "carillon.redisUrl" -}}
{{- if .Values.valkey.enabled -}}
{{- printf "redis://%s:6379/0" (include "carillon.valkeyFullname" .) -}}
{{- else -}}
{{- .Values.externalRedisUrl -}}
{{- end -}}
{{- end -}}
