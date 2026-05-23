# claude-k8s-agent

Autonomer Claude Code Agent für Kubernetes (k3s / Homelab).

Arbeitet GitHub Issues selbstständig ab, öffnet Pull Requests und sendet
Push-Benachrichtigungen via ntfy.sh. Der Nutzer kann jederzeit per Claude.ai
App (Mobile / Browser) in die laufende Session eingreifen.

## Überblick

- **Issue Controller** — CronJob alle 10 Minuten: liest GitHub Issues mit Label , spawnt Kubernetes Jobs
- **Agent Job** — pro Issue ein Pod:  →  → 
- **ntfy.sh** — self-hosted Push-Notifications mit Session-URL direkt aufs Handy
- **ArgoCD** — GitOps-Deployment ins Homelab via ArgoCD

## Konfiguration pro Ziel-Repo

Lege eine  im Root deines Ziel-Repos an:

```yaml
forgecrate:
  profile: fullstack   # backend | frontend | fullstack
  flavors:
    - tdd
    - github
    - gitops
```

Fehlt die Datei, greifen die ConfigMap-Defaults (, ).

## Dokumentation

- [Design-Spec](docs/superpowers/specs/2026-05-24-autonomous-claude-k8s-design.md)

## Status

> Planung abgeschlossen — Implementierung in Vorbereitung
