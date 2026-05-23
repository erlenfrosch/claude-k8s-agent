# Design: Autonomer Claude Code Agent in Kubernetes

**Datum:** 2026-05-24
**Status:** Zur Implementierung freigegeben

## Zusammenfassung

Ein selbstständig arbeitender Claude Code Agent, der in einem k3s-Homelab-Cluster laeuft
und GitHub Issues autonom abarbeitet. Der Nutzer erhaelt Push-Benachrichtigungen via ntfy.sh
und kann bei Bedarf vom Handy aus ueber claude.ai direkt in die laufende Session eingreifen.

## Architektur: Komponenten-Uebersicht

| Komponente | Typ | Zweck |
|---|---|---|
| issue-controller | CronJob (10 min) | GitHub Issues zu Kubernetes Jobs spawnen |
| claude-agent | Kubernetes Job | Autonomer Claude Code Agent pro Issue |
| ntfy | Deployment + Ingress + PVC | Self-hosted Push-Notification-Service |
| Agent-Container-Image | OCI Image via ghcr.io | Ubuntu + forgecrate + claude code |
| .claude-agent.yaml | Datei im Ziel-Repo | forgecrate Profil/Flavors pro Repo |
| claude-agent-config | ConfigMap | max-pods, defaults, ntfy-topic |
| claude-agent-secrets | SealedSecret | Anthropic API Key, GitHub PAT, ntfy Token |
| ArgoCD Application | GitOps Manifest | Deployment ins Homelab via ArgoCD |

## Datenfluss

1. CronJob liest alle 10 min GitHub Issues mit Label agent-backlog
2. Wenn laufende Jobs < max-pods: neuer Kubernetes Job pro Issue
3. Job: git clone + forgecrate init (Profil/Flavors aus .claude-agent.yaml)
4. Claude Code startet mit --share --dangerously-skip-permissions
5. Share-URL wird extrahiert, ntfy-Notification gesendet
6. Agent arbeitet autonom, oeffnet PR, schliesst Issue
7. Bei Abschluss/Fehler: erneute ntfy-Notification
8. Nutzer tippt auf Notification, Claude.ai App oeffnet Session

## 1. Issue Controller (CronJob)

**Interval:** 10 Minuten

**Logik pro Lauf:**
1. Lies ConfigMap: target-repo, label-filter, max-pods, defaults
2. GitHub API: offene Issues mit Label agent-backlog (nicht agent-running)
3. K8s API: zaehle Jobs mit Label app=claude-agent
4. Solange running < max-pods UND Issues vorhanden:
   - Erstelle Job claude-agent-issue-N
   - Setze Label: agent-backlog -> agent-running
5. Bei Job-Fehler: Label zurueck auf agent-backlog + Failure-Notification

**Job-Naming:** claude-agent-issue-N (verhindert Doppel-Spawning)
**Priorisierung:** aeltestes Issue zuerst

**RBAC (namespace-scoped):**
- batch/jobs: create, get, list, delete
- core/configmaps: get

## 2. Agent Job Pod

**restartPolicy:** Never
**ttlSecondsAfterFinished:** 3600

### Init-Container setup

1. git clone via PAT in /workspace
2. Lese .claude-agent.yaml (oder ConfigMap-Defaults)
3. forgecrate init --profile PROFILE --flavors FLAVORS
4. Schreibe /workspace/.agent-prompt mit Issue-Kontext

### .agent-prompt Inhalt

GitHub Issue N: TITLE
BODY

Deine Aufgabe: Arbeite dieses Issue vollstaendig ab.
Erstelle Branch agent/issue-N, committe Aenderungen, oeffne einen PR.
Repo: TARGET_REPO

### Main-Container claude-agent

entrypoint.sh:
- Startet claude mit --dangerously-skip-permissions --share --message
- Extrahiert Share-URL aus stdout (grep https://claude.ai)
- Sendet sofortige ntfy-Notification mit Session-URL + Issue-Titel
- Wartet auf Claude-Prozess
- Sendet Abschluss- oder Failure-Notification

**securityContext:** runAsUser: 0 (root, max. lokale Berechtigungen)
**resources:** requests 2Gi/500m, limits 4Gi/2000m

**Env-Variablen:**
- ANTHROPIC_API_KEY (Secret)
- GITHUB_TOKEN (Secret)
- NTFY_URL, NTFY_TOPIC (ConfigMap)
- NTFY_AUTH_TOKEN (Secret)
- ISSUE_NUMBER, ISSUE_TITLE, TARGET_REPO (vom Controller)

## 3. Notification-Typen

| Ereignis | Titel | Prioritaet |
|---|---|---|
| Session gestartet | Agent laeuft: Issue N + Session-URL | default |
| Braucht Input | Eingabe noetig: Issue N + Session-URL | high |
| Abgeschlossen | PR geoeffnet: Issue N + PR-URL | default |
| Fehler | Agent fehlgeschlagen: Issue N + Log | urgent |

## 4. ntfy.sh Deployment

- Image: binwiederhier/ntfy:latest
- Konfiguration: base-url, auth-default-access: deny-all, behind-proxy: true
- PVC: 1Gi fuer Cache
- Ingress: TLS via cert-manager
- Client: ntfy-App (iOS/Android), Topic claude-agent mit Auth-Token

## 5. forgecrate Integration

### Container-Image (ubuntu:24.04, ghcr.io)

Vorinstalliert: git, curl, nodejs, npm, golang-go, jq, forgecrate (apt jmt-labs), claude code (npm)

### Per-Repo Konfiguration (.claude-agent.yaml)



Fallback wenn fehlend: ConfigMap-Defaults (profile: backend, flavors: github,gitops)

### MCP-Server

forgecrate-generierte MCP-Server (github, memory-bank, context7) laufen normal.
GITHUB_PERSONAL_ACCESS_TOKEN aus K8s Secret als Env-Variable injiziert.

## 6. ConfigMap claude-agent-config



## 7. Secrets (SealedSecret)

ANTHROPIC_API_KEY, GITHUB_TOKEN, NTFY_AUTH_TOKEN (alle kubeseal-verschluesselt)

## 8. GitOps-Struktur (ArgoCD)



ArgoCD Application: syncPolicy automated, prune: true, selfHeal: true

## Nicht im Scope

- Multi-Cluster / Federation
- GitHub Webhook-Trigger (CronJob genuegt fuer Anfang)
- Persistentes Workspace-Volume (frischer Clone pro Job ist sauberer)
- Eigener Kubernetes Operator (CronJob genuegt)

## Offene Entscheidungen fuer die Implementierung

- Domain fuer ntfy-Ingress festlegen
- ghcr.io Repository-Name fuer Agent-Image
- GitHub PAT Scopes: repo + issues + pull_requests
- Issue-Controller: Go-Binary (empfohlen) oder Shell-Script mit gh CLI
