#!/bin/bash
set -euo pipefail

CLAUDE_LOG=/tmp/claude.log
WORKSPACE=/workspace
WATCHDOG_INTERVAL=60     # Sekunden zwischen Watchdog-Prüfungen
WATCHDOG_IDLE_ROUNDS=10  # Runden ohne Output = ~10 Min -> "needs input"
NOTIFIED_IDLE=false
SHARE_URL=""

gotify_send() {
  local title="$1" body="$2" priority="${3:-default}"
  # Priorität auf Gotify-Integer mappen (default=5, high=7, urgent=9)
  local prio=5
  case "$priority" in high) prio=7 ;; urgent) prio=9 ;; esac
  # JSON sicher über python3 bauen (vermeidet Quote-Injection in title/body)
  local payload
  payload=$(python3 -c "
import json, sys
print(json.dumps({'title': sys.argv[1], 'message': sys.argv[2], 'priority': int(sys.argv[3])}))
" "$title" "$body" "$prio")
  curl -s -X POST \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "${GOTIFY_URL}/message?token=${GOTIFY_TOKEN:-}" || {
    echo "WARNUNG: Gotify-Notification fehlgeschlagen" >&2
    true
  }
}

extract_share_url() {
  grep -o 'https://claude\.ai/[^ "]*' "$CLAUDE_LOG" 2>/dev/null \
    | grep -v 'accounts' | head -1 || true
}

cd "$WORKSPACE"

echo "=== Agent startet: Issue #${ISSUE_NUMBER}: ${ISSUE_TITLE} ==="
echo "=== Repo: ${TARGET_REPO} ==="

# OAuth-Credentials aus Secret einlesen (Alternative zu ANTHROPIC_API_KEY).
# Ermöglicht Nutzung eines Claude.ai Pro/Max-Abos ohne API-Kosten.
# Inhalt: ~/.claude/.credentials.json vom Entwickler-Rechner, als K8s-SealedSecret gespeichert.
if [ -n "${CLAUDE_CREDENTIALS:-}" ]; then
  mkdir -p "${HOME:-/root}/.claude"
  printf '%s' "$CLAUDE_CREDENTIALS" > "${HOME:-/root}/.claude/.credentials.json"
  chmod 600 "${HOME:-/root}/.claude/.credentials.json"
  echo "=== Claude OAuth-Credentials aus Secret geladen ==="
fi

# Preflight: .agent-prompt muss vorhanden sein (wird vom Init-Container erstellt)
if [ ! -f .agent-prompt ]; then
  echo "FEHLER: .agent-prompt fehlt — Init-Container hat nicht korrekt abgeschlossen" >&2
  gotify_send "Fehler: Issue #${ISSUE_NUMBER}" \
    ".agent-prompt fehlt im Workspace. Init-Container prüfen." "urgent"
  exit 1
fi

# Claude Code starten
claude \
  --dangerously-skip-permissions \
  --share \
  --message "$(cat .agent-prompt)" \
  2>&1 | tee "$CLAUDE_LOG" &

CLAUDE_PID=$!

# Share-URL abwarten (max 60 Sekunden)
for i in $(seq 1 30); do
  SHARE_URL=$(extract_share_url)
  if [ -n "$SHARE_URL" ]; then
    echo "Share-URL: $SHARE_URL"
    break
  fi
  sleep 2
done

# Start-Notification
ISSUE_HEADER=$(head -1 .agent-prompt 2>/dev/null || echo "Issue #${ISSUE_NUMBER}")
if [ -n "$SHARE_URL" ]; then
  gotify_send "Agent laeuft: Issue #${ISSUE_NUMBER}" \
    "${ISSUE_HEADER}
Session: ${SHARE_URL}" "default"
else
  gotify_send "Agent laeuft: Issue #${ISSUE_NUMBER}" \
    "${ISSUE_HEADER}
(Kein Share-Link verfuegbar)" "default"
fi

# Watchdog
LAST_SIZE=0
IDLE_ROUNDS=0

while kill -0 $CLAUDE_PID 2>/dev/null; do
  sleep "$WATCHDOG_INTERVAL"
  CURRENT_SIZE=$(wc -c < "$CLAUDE_LOG" 2>/dev/null || echo 0)

  if [ "$CURRENT_SIZE" -eq "$LAST_SIZE" ]; then
    IDLE_ROUNDS=$((IDLE_ROUNDS + 1))
    if [ "$IDLE_ROUNDS" -ge "$WATCHDOG_IDLE_ROUNDS" ] && [ "$NOTIFIED_IDLE" = "false" ]; then
      echo "Watchdog: Agent idle seit ${WATCHDOG_IDLE_ROUNDS} Runden"
      gotify_send "Eingabe noetig: Issue #${ISSUE_NUMBER}" \
        "Agent wartet auf Eingabe.
Session: ${SHARE_URL:-kein Link}" "high"
      NOTIFIED_IDLE=true
    fi
  else
    IDLE_ROUNDS=0
    NOTIFIED_IDLE=false
    LAST_SIZE=$CURRENT_SIZE
  fi
done

wait $CLAUDE_PID
EXIT_CODE=$?

if [ $EXIT_CODE -eq 0 ]; then
  PR_URL=$(grep -o 'https://github\.com/[^ "]*pull[^ "]*' "$CLAUDE_LOG" | tail -1 || true)
  BODY="${PR_URL:-Issue #${ISSUE_NUMBER} abgeschlossen (kein PR-Link im Log)}"
  gotify_send "Abgeschlossen: Issue #${ISSUE_NUMBER}" "$BODY" "default"
  echo "=== Agent erfolgreich ==="
else
  LAST_LINES=$(tail -15 "$CLAUDE_LOG" 2>/dev/null || echo "Kein Log")
  gotify_send "Fehler: Issue #${ISSUE_NUMBER}" \
    "Exit-Code: ${EXIT_CODE}
${LAST_LINES}" "urgent"
  echo "=== Agent fehlgeschlagen: Exit ${EXIT_CODE} ===" >&2
  exit $EXIT_CODE
fi
