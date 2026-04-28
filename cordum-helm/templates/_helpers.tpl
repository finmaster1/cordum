{{- define "cordum.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "cordum.fullname" -}}
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

{{- define "cordum.labels" -}}
app.kubernetes.io/name: {{ include "cordum.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "cordum.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cordum.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "cordum.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- if .Values.serviceAccount.name -}}
{{- .Values.serviceAccount.name -}}
{{- else -}}
{{- printf "%s" (include "cordum.fullname" .) -}}
{{- end -}}
{{- else -}}
{{- .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "cordum.natsUrl" -}}
{{- if .Values.nats.enabled -}}
{{- printf "nats://%s-nats:%d" (include "cordum.fullname" .) (int .Values.nats.service.port) -}}
{{- else -}}
{{- required "external.natsUrl is required when nats.enabled=false" .Values.external.natsUrl -}}
{{- end -}}
{{- end -}}

{{- define "cordum.redisUrl" -}}
{{- if .Values.redis.enabled -}}
{{- if .Values.redis.auth.enabled -}}
{{- printf "redis://:$(REDIS_PASSWORD)@%s-redis:%d" (include "cordum.fullname" .) (int .Values.redis.service.port) -}}
{{- else -}}
{{- printf "redis://%s-redis:%d" (include "cordum.fullname" .) (int .Values.redis.service.port) -}}
{{- end -}}
{{- else -}}
{{- required "external.redisUrl is required when redis.enabled=false" .Values.external.redisUrl -}}
{{- end -}}
{{- end -}}

{{- define "cordum.redisSecretName" -}}
{{- if .Values.redis.auth.existingSecret -}}
{{- .Values.redis.auth.existingSecret -}}
{{- else -}}
{{- printf "%s-secrets" (include "cordum.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "cordum.redisSecretKey" -}}
{{- if .Values.redis.auth.existingSecret -}}
{{- .Values.redis.auth.existingSecretKey -}}
{{- else -}}
redisPassword
{{- end -}}
{{- end -}}

{{- define "cordum.licenseSecretName" -}}
{{- if .Values.licensing.existingSecret -}}
{{- .Values.licensing.existingSecret -}}
{{- else -}}
{{- printf "%s-license" (include "cordum.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "cordum.natsTokenSecretName" -}}
{{- if .Values.nats.auth.existingTokenSecret -}}
{{- .Values.nats.auth.existingTokenSecret -}}
{{- else -}}
{{- printf "%s-nats-token" (include "cordum.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "cordum.jwtSecretName" -}}
{{- printf "%s-jwt" (include "cordum.fullname" .) -}}
{{- end -}}

{{- define "cordum.auditWebhookSecretName" -}}
{{- printf "%s-audit-webhook" (include "cordum.fullname" .) -}}
{{- end -}}

{{- define "cordum.auditDatadogSecretName" -}}
{{- printf "%s-audit-datadog" (include "cordum.fullname" .) -}}
{{- end -}}

{{- define "cordum.inferenceBackend" -}}
{{- $backend := default "ollama-cpu" .Values.inference.backend -}}
{{- if not (or (eq $backend "ollama-cpu") (eq $backend "vllm-gpu")) -}}
{{- fail (printf "unsupported inference.backend=%s; allowed: ollama-cpu, vllm-gpu" $backend) -}}
{{- end -}}
{{- $backend -}}
{{- end -}}

{{/*
Shared environment injected into every control-plane container. Keep this
list to cross-cutting runtime knobs only; service-specific endpoints and
secrets stay near the service that consumes them.
*/}}
{{- define "cordum.sharedEnv" -}}
- name: CORDUM_LOG_LEVEL
  value: {{ .Values.logging.level | quote }}
- name: CORDUM_LOG_FORMAT
  value: {{ .Values.logging.format | quote }}
{{- if .Values.licensing.mode }}
- name: CORDUM_LICENSE_MODE
  value: {{ .Values.licensing.mode | quote }}
{{- end }}
{{- if .Values.licensing.file }}
- name: CORDUM_LICENSE_FILE
  value: {{ .Values.licensing.file | quote }}
{{- end }}
{{- if or .Values.licensing.token .Values.licensing.existingSecret }}
- name: CORDUM_LICENSE_TOKEN
  valueFrom:
    secretKeyRef:
      name: {{ include "cordum.licenseSecretName" . }}
      key: license.json
{{- end }}
{{- if .Values.licensing.publicKey }}
- name: CORDUM_LICENSE_PUBLIC_KEY
  value: {{ .Values.licensing.publicKey | quote }}
{{- end }}
{{- if .Values.licensing.publicKeyPath }}
- name: CORDUM_LICENSE_PUBLIC_KEY_PATH
  value: {{ .Values.licensing.publicKeyPath | quote }}
{{- end }}
{{- if .Values.marketplace.provider }}
- name: CORDUM_MARKETPLACE_PROVIDER
  value: {{ .Values.marketplace.provider | quote }}
{{- end }}
{{- if .Values.marketplace.allowHttp }}
- name: CORDUM_MARKETPLACE_ALLOW_HTTP
  value: "true"
{{- end }}
{{- if .Values.marketplace.httpTimeout }}
- name: CORDUM_MARKETPLACE_HTTP_TIMEOUT
  value: {{ .Values.marketplace.httpTimeout | quote }}
{{- end }}
{{- if .Values.outputPolicy.enabled }}
- name: OUTPUT_POLICY_ENABLED
  value: "true"
{{- end }}
{{- if .Values.outputPolicy.failMode }}
- name: OUTPUT_POLICY_FAIL_MODE
  value: {{ .Values.outputPolicy.failMode | quote }}
{{- end }}
{{- if .Values.outputPolicy.scannersPath }}
- name: OUTPUT_SCANNERS_PATH
  value: {{ .Values.outputPolicy.scannersPath | quote }}
{{- end }}
{{- if .Values.auth.apiKeys }}
- name: CORDUM_API_KEYS
  value: {{ .Values.auth.apiKeys | quote }}
{{- end }}
{{- if .Values.auth.apiKeysPath }}
- name: CORDUM_API_KEYS_PATH
  value: {{ .Values.auth.apiKeysPath | quote }}
{{- end }}
{{- if .Values.auth.skipSecretValidation }}
- name: CORDUM_SKIP_SECRET_VALIDATION
  value: "true"
{{- end }}
{{- if .Values.auth.jwt.hmacSecret }}
- name: CORDUM_JWT_HMAC_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "cordum.jwtSecretName" . }}
      key: hmacSecret
{{- end }}
{{- if .Values.auth.jwt.publicKey }}
- name: CORDUM_JWT_PUBLIC_KEY
  value: {{ .Values.auth.jwt.publicKey | quote }}
{{- end }}
{{- if .Values.auth.jwt.publicKeyPath }}
- name: CORDUM_JWT_PUBLIC_KEY_PATH
  value: {{ .Values.auth.jwt.publicKeyPath | quote }}
{{- end }}
{{- if .Values.auth.jwt.issuer }}
- name: CORDUM_JWT_ISSUER
  value: {{ .Values.auth.jwt.issuer | quote }}
{{- end }}
{{- if .Values.auth.jwt.audience }}
- name: CORDUM_JWT_AUDIENCE
  value: {{ .Values.auth.jwt.audience | quote }}
{{- end }}
{{- if .Values.auth.jwt.defaultRole }}
- name: CORDUM_JWT_DEFAULT_ROLE
  value: {{ .Values.auth.jwt.defaultRole | quote }}
{{- end }}
{{- if .Values.auth.jwt.clockSkew }}
- name: CORDUM_JWT_CLOCK_SKEW
  value: {{ .Values.auth.jwt.clockSkew | quote }}
{{- end }}
{{- if .Values.auth.jwt.required }}
- name: CORDUM_JWT_REQUIRED
  value: "true"
{{- end }}
{{- end -}}

{{/*
Production safety validations — hard-fail on dangerous combinations.
TLS is mandatory in production mode; network policies and persistence
are warned about in NOTES.txt but not blocked (legitimate use cases exist).
*/}}
{{- define "cordum.validateProductionConfig" -}}
{{- if and .Values.global.production (not .Values.global.tls.enabled) -}}
{{- fail "FATAL: TLS must be enabled in production mode (global.production=true requires global.tls.enabled=true)" -}}
{{- end -}}
{{- if and .Values.global.production .Values.redis.auth.enabled (not .Values.redis.auth.password) (not .Values.redis.auth.existingSecret) -}}
{{- fail "FATAL: Redis auth is enabled in production mode but no password or existingSecret is configured" -}}
{{- end -}}
{{- end -}}

{{- define "cordum.safetyKernelAddr" -}}
{{- if .Values.safetyKernel.enabled -}}
{{- printf "%s-safety-kernel:%d" (include "cordum.fullname" .) (int .Values.safetyKernel.service.port) -}}
{{- else -}}
{{- required "external.safetyKernelAddr is required when safetyKernel.enabled=false" .Values.external.safetyKernelAddr -}}
{{- end -}}
{{- end -}}
