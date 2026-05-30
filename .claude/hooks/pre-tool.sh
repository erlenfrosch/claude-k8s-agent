#!/usr/bin/env bash
# PreToolUse-Hook. Erhält das Hook-JSON über stdin (tool_name, tool_input,
# transcript_path). stdin darf nur einmal gelesen werden — daher in STDIN_JSON puffern
# und an `forgecrate hook require-research` weiterreichen.

STDIN_JSON=""
if [ ! -t 0 ]; then
  STDIN_JSON=$(cat)
fi

# tool_name aus dem stdin-JSON; Fallback auf die (inoffizielle) Env-Var.
TOOL=$(printf '%s' "$STDIN_JSON" | sed -n 's/.*"tool_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')
if [ -z "$TOOL" ]; then
  TOOL="${CLAUDE_TOOL_NAME:-}"
fi

# require_research ruft die Go-Binary auf; bei nicht-leerer Ausgabe (deny-JSON)
# wird diese ausgegeben und der Tool-Aufruf blockiert. Fail-open ohne Binary.
require_research() {
  if command -v forgecrate >/dev/null 2>&1; then
    DECISION=$(printf '%s' "$STDIN_JSON" | forgecrate hook require-research)
    if [ -n "$DECISION" ]; then
      printf '%s' "$DECISION"
      exit 0
    fi
  fi
}

case "$TOOL" in
  Edit|Write|MultiEdit)
    BRANCH=$(git branch --show-current 2>/dev/null)
    if [ "$BRANCH" = "main" ] || [ "$BRANCH" = "master" ]; then
      printf '{"continue": false, "stopReason": "Direkte Änderungen auf main sind verboten. Branch anlegen: git checkout -b feat/<thema> — dann erst Edit/Write verwenden."}'
      exit 0
    fi
    require_research
    echo '{"hookSpecificOutput": {"hookEventName": "PreToolUse", "additionalContext": "Branch-Check OK. Stelle sicher: brainstorming und tdd Skills wurden aufgerufen."}}'
    ;;
  Bash)
    BRANCH=$(git branch --show-current 2>/dev/null)
    if [ "$BRANCH" = "main" ] || [ "$BRANCH" = "master" ]; then
      INPUT=$(printf '%s' "$STDIN_JSON" | sed -n 's/.*"command"[[:space:]]*:[[:space:]]*"\(.*\)".*/\1/p')
      if [ -z "$INPUT" ]; then
        INPUT="${TOOL_INPUT:-}"
      fi

      # git commit direkt auf main
      if echo "$INPUT" | grep -qE '(^|[;&|]|\brun\b)\s*git\s+commit\b'; then
        printf '{"continue": false, "stopReason": "Destruktiver Bash-Befehl auf main verboten: git commit. Branch anlegen: git checkout -b feat/<thema>"}'
        exit 0
      fi

      # git push --force / git push -f
      if echo "$INPUT" | grep -qE 'git\s+push\s+.*(-f\b|--force\b)'; then
        printf '{"continue": false, "stopReason": "Destruktiver Bash-Befehl auf main verboten: git push --force. Force-Push auf main ist nicht erlaubt."}'
        exit 0
      fi

      # git push origin main / git push origin master
      if echo "$INPUT" | grep -qE 'git\s+push\b.*\b(main|master)\b'; then
        printf '{"continue": false, "stopReason": "Destruktiver Bash-Befehl auf main verboten: git push ... main/master. Branch anlegen: git checkout -b feat/<thema>"}'
        exit 0
      fi

      # git reset --hard
      if echo "$INPUT" | grep -qE 'git\s+reset\s+--hard\b'; then
        printf '{"continue": false, "stopReason": "Destruktiver Bash-Befehl auf main verboten: git reset --hard."}'
        exit 0
      fi

      # git clean -f
      if echo "$INPUT" | grep -qE 'git\s+clean\s+.*-[a-zA-Z]*f'; then
        printf '{"continue": false, "stopReason": "Destruktiver Bash-Befehl auf main verboten: git clean -f."}'
        exit 0
      fi

      # Schreib-Redirectionen in versionierte Dateien (nicht /tmp/).
      # >&N (fd-Redirects wie 2>&1) werden nicht geblockt: & aus Zeichenklasse ausgeschlossen.
      if echo "$INPUT" | grep -qE '>+\s*[^/\s&][^\s]*' && ! echo "$INPUT" | grep -qE '>+\s*/tmp/'; then
        printf '{"continue": false, "stopReason": "Schreib-Redirektion (> oder >>) in versionierte Datei auf main verboten. Branch anlegen oder /tmp/ verwenden."}'
        exit 0
      fi
    fi

    # force-research: schreibende Bash-Befehle ohne vorherige Recherche blocken
    require_research
    echo '{"hookSpecificOutput": {"hookEventName": "PreToolUse", "additionalContext": "Bash-Aufruf. Keine destruktiven Aktionen ohne Bestätigung."}}'
    ;;
esac

# GEBLOCKTE Bash-Muster auf main/master:
# - git commit (direkter Commit auf main)
# - git push [--force] origin main
# - git reset --hard
# - Schreib-Redirectionen (>> / >)
# Recherche-Block (require-research): Edit/Write/MultiEdit immer, schreibende Bash bei
# Flavor force-research — bis im aktuellen Turn ein Recherche-Tool genutzt wurde.
# Hinweis: Dieser Hook ist keine alleinige Schutzschicht. GitHub Branch Protection ergänzen.
