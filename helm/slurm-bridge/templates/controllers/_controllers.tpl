{{- /*
SPDX-FileCopyrightText: Copyright (C) SchedMD LLC.
SPDX-License-Identifier: Apache-2.0
*/}}

{{/*
Determine bridge controllers name
*/}}
{{- define "slurm-bridge.controllers.name" -}}
{{ printf "%s-controllers" (include "slurm-bridge.name" .) }}
{{- end }}

{{/*
Determine bridge controllers image repository
*/}}
{{- define "slurm-bridge.controllers.image.repository" -}}
{{ .Values.controllers.image.repository | default (printf "ghcr.io/slinkyproject/%s" (include "slurm-bridge.controllers.name" .)) }}
{{- end }}

{{/*
Define bridge controllers image tag
*/}}
{{- define "slurm-bridge.controllers.image.tag" -}}
{{ .Values.controllers.image.tag | default .Chart.Version }}
{{- end }}

{{/*
Determine bridge controllers image reference (repo:tag)
*/}}
{{- define "slurm-bridge.controllers.imageRef" -}}
{{- if .Values.controllers.image.digest -}}
{{ printf "%s@%s" (include "slurm-bridge.controllers.image.repository" .) .Values.controllers.image.digest | quote }}
{{- else -}}
{{ printf "%s:%s" (include "slurm-bridge.controllers.image.repository" .) (include "slurm-bridge.controllers.image.tag" .) | quote }}
{{- end -}}
{{- end }}

{{/*
The controllers labels
*/}}
{{- define "slurm-bridge.controllers.labels" -}}
{{ include "slurm-bridge.labels" . }}
{{ include "slurm-bridge.controllers.selectorLabels" . }}
app.kubernetes.io/component: controllers
{{- end }}

{{/*
The controllers selector labels
*/}}
{{- define "slurm-bridge.controllers.selectorLabels" -}}
app.kubernetes.io/name: {{ include "slurm-bridge.controllers.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
