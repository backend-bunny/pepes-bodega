{{- define "bodega.fullname" -}}
{{- printf "%s" .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "bodega.labels" -}}
app.kubernetes.io/part-of: pepes-bodega
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "bodega.selector" -}}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- define "bodega.apiImage" -}}
{{ .Values.image.api.repository }}:{{ .Values.image.api.tag | default .Chart.AppVersion }}
{{- end -}}

{{- define "bodega.workerImage" -}}
{{ .Values.image.worker.repository }}:{{ .Values.image.worker.tag | default .Chart.AppVersion }}
{{- end -}}

{{/* Env shared by API and workers. */}}
{{- define "bodega.env" -}}
- name: BODEGA_NATS_URL
  value: {{ .Values.config.natsURL | quote }}
- name: BODEGA_S3_ENDPOINT
  value: {{ .Values.config.s3.endpoint | quote }}
- name: BODEGA_S3_REGION
  value: {{ .Values.config.s3.region | quote }}
- name: BODEGA_S3_BUCKET
  value: {{ .Values.config.s3.bucket | quote }}
- name: BODEGA_S3_USE_SSL
  value: {{ .Values.config.s3.useSSL | quote }}
- name: BODEGA_S3_ACCESS_KEY
  valueFrom: { secretKeyRef: { name: {{ .Values.existingSecret }}, key: BODEGA_S3_ACCESS_KEY } }
- name: BODEGA_S3_SECRET_KEY
  valueFrom: { secretKeyRef: { name: {{ .Values.existingSecret }}, key: BODEGA_S3_SECRET_KEY } }
- name: BODEGA_DATABASE_URL
{{- if .Values.cnpg.enabled }}
  valueFrom: { secretKeyRef: { name: {{ include "bodega.fullname" . }}-db-app, key: uri } }
{{- else }}
  valueFrom: { secretKeyRef: { name: {{ .Values.existingSecret }}, key: BODEGA_DATABASE_URL } }
{{- end }}
{{- with .Values.extraEnv }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/* Env for Python worker containers. */}}
{{- define "bodega.workerEnv" -}}
{{ include "bodega.env" . }}
- name: BODEGA_CLIP_MODEL
  value: {{ .Values.config.workers.clipModel | quote }}
- name: BODEGA_CLIP_PRETRAINED
  value: {{ .Values.config.workers.clipPretrained | quote }}
- name: BODEGA_WHISPER_MODEL
  value: {{ .Values.config.workers.whisperModel | quote }}
- name: BODEGA_MAX_FRAMES
  value: {{ .Values.config.workers.maxFrames | quote }}
- name: BODEGA_FRAME_INTERVAL_S
  value: {{ .Values.config.workers.frameIntervalS | quote }}
- name: BODEGA_DUP_THRESHOLD
  value: {{ .Values.config.workers.dupThreshold | quote }}
{{- end -}}

{{- define "bodega.podSecurity" -}}
securityContext:
  runAsNonRoot: true
  seccompProfile: { type: RuntimeDefault }
{{- with .Values.nodeSelector }}
nodeSelector: {{ toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.tolerations }}
tolerations: {{ toYaml . | nindent 2 }}
{{- end }}
{{- with .Values.imagePullSecrets }}
imagePullSecrets: {{ toYaml . | nindent 2 }}
{{- end }}
{{- end -}}

{{- define "bodega.containerSecurity" -}}
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities: { drop: [ALL] }
{{- end -}}
