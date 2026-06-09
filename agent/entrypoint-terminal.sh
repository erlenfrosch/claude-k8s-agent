#!/bin/bash
set -euo pipefail

# sshd benötigt dieses Verzeichnis für Privilege-Separation-State
mkdir -p /var/run/sshd

# SSH Host-Keys generieren (idempotent — überschreibt keine vorhandenen Keys)
ssh-keygen -A

# Container-gerechte sshd-Konfiguration anhängen
cat >> /etc/ssh/sshd_config <<'EOF'
PasswordAuthentication no
AllowUsers ubuntu
UsePrivilegeSeparation no
UsePAM no
EOF

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
