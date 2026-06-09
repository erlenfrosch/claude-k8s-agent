#!/bin/bash
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then
    echo "FEHLER: entrypoint-terminal.sh muss als root laufen (securityContext.runAsUser: 0)" >&2
    exit 1
fi

# sshd benötigt dieses Verzeichnis für Privilege-Separation-State
mkdir -p /var/run/sshd

# SSH Host-Keys generieren (idempotent — überschreibt keine vorhandenen Keys)
ssh-keygen -A

# Container-gerechte sshd-Konfiguration als Drop-in (idempotent)
mkdir -p /etc/ssh/sshd_config.d
cat > /etc/ssh/sshd_config.d/99-terminal-pod.conf <<'EOF'
PasswordAuthentication no
AllowUsers ubuntu
UsePrivilegeSeparation no
UsePAM no
EOF

if [ -z "${SSH_PUBLIC_KEY:-}" ]; then
    echo "FEHLER: SSH_PUBLIC_KEY nicht gesetzt" >&2
    exit 1
fi

# authorized_keys für ubuntu aus Secret-Env-Var befüllen
mkdir -p /home/ubuntu/.ssh
printf '%s\n' "${SSH_PUBLIC_KEY}" > /home/ubuntu/.ssh/authorized_keys
chmod 700 /home/ubuntu/.ssh
chmod 600 /home/ubuntu/.ssh/authorized_keys
chown -R ubuntu:ubuntu /home/ubuntu/.ssh

# CLAUDE_CODE_OAUTH_TOKEN in .bashrc schreiben damit SSH-Sessions es erben
if [ -n "${CLAUDE_CODE_OAUTH_TOKEN:-}" ]; then
    printf 'export CLAUDE_CODE_OAUTH_TOKEN=%q\n' "${CLAUDE_CODE_OAUTH_TOKEN}" \
        >> /home/ubuntu/.bashrc
    chown ubuntu:ubuntu /home/ubuntu/.bashrc
fi

exec /usr/sbin/sshd -D -e -p 2222
