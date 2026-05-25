# claude-k8s-agent

Autonomer Claude Code Agent fuer Kubernetes (k3s / Homelab).

Arbeitet GitHub Issues selbststaendig ab, oeffnet Pull Requests und sendet
Push-Benachrichtigungen via ntfy.sh. Jederzeit per Claude.ai App (Mobile /
Browser) in die laufende Session eingreifbar.

## Wie es funktioniert

1. CronJob prueft alle 10 Minuten Issues mit Label `agent-backlog`
2. Pro Issue wird ein K8s Job gespawnt (max. konfigurierbar, Standard: 4)
3. Job: `git clone` + `forgecrate init` + `claude --share --dangerously-skip-permissions`
4. Agent arbeitet autonom: Tests, Implementierung, PR oeffnen
5. Push-Notification via ntfy.sh mit Claude.ai Session-URL
6. Tap auf Notification -> Claude.ai App -> Session interaktiv fortfuehren

## Schnellstart

**Voraussetzungen:** k3s + ArgoCD + SealedSecrets, GitHub PAT (repo + issues),
Anthropic API Key, Domain mit TLS.

### 1. Secrets verschluesseln und committen

```bash
# /tmp/secret.yaml anlegen (NICHT committen):
cat > /tmp/secret.yaml << EOF
apiVersion: v1
kind: Secret
metadata:
  name: claude-agent-secrets
  namespace: claude-agent
stringData:
  ANTHROPIC_API_KEY: "sk-ant-..."
  GITHUB_TOKEN: "ghp_..."
  NTFY_AUTH_TOKEN: "dein-ntfy-token"
EOF

kubeseal --namespace claude-agent --name claude-agent-secrets \
  < /tmp/secret.yaml > deploy/sealed-secret.yaml
rm /tmp/secret.yaml
git add deploy/sealed-secret.yaml && git commit -m "feat: add sealed secrets"
git push origin main
```

### 2. Konfiguration anpassen

- `deploy/configmap.yaml`: `NTFY_URL`, `TARGET_REPO` setzen
- `deploy/ntfy/deployment.yaml` + `ingress.yaml`: `DEINE_DOMAIN` ersetzen

### 3. ArgoCD Application deployen

```bash
kubectl apply -f deploy/argocd/application.yaml
```

ArgoCD synct automatisch alle Manifeste aus `deploy/`.

### 4. Erstes Issue anlegen

Im Ziel-Repo ein GitHub Issue anlegen und Label `agent-backlog` setzen.
Innerhalb von 10 Minuten startet der Agent automatisch.

## Per-Repo Konfiguration

`.claude-agent.yaml` im Root des Ziel-Repos anlegen (Vorlage: `.claude-agent.yaml.example`):

```yaml
forgecrate:
  profile: fullstack
  flavors:
    - tdd
    - github
    - gitops
```

## Dokumentation

- [Design-Spec](docs/superpowers/specs/2026-05-24-autonomous-claude-k8s-design.md)
- [Implementierungsplan](docs/superpowers/plans/2026-05-24-autonomous-claude-k8s-agent.md)
