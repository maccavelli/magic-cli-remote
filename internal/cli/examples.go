package cli

// Centralized CLI examples shown via cobra Example / Long help.
// Keep in sync with README.md and docs/config.md.

const rootExample = `
  # Version
  mcremote version
  mcremote --version

  # Foreground daemon
  mcremote serve
  mcremote serve --listen-host tailscale --listen-port 7531
  mcremote serve --config ~/.config/mcremote/config.yaml --log-level debug
  mcremote serve --listen-host 127.0.0.1 --listen-port 7531 --data-dir ~/.local/share/mcremote
  mcremote serve --log-format json

  # Install systemd --user service (recommended on Linux)
  make install
  mcremote --setup-service
  mcremote setup-service
  mcremote setup-service --force
  mcremote setup-service --print-only
  mcremote setup-service --listen-host tailscale --listen-port 7531 --force
  mcremote setup-service --config ~/.config/mcremote/config.yaml --force
  mcremote setup-service --config /path/to/configs/config.mesh-grok.yaml --force
  mcremote setup-service --env MCREMOTE_LOG_LEVEL=debug --force
  mcremote setup-service --binary ~/.local/bin/mcremote --force
  mcremote setup-service --no-start --no-enable --print-only

  # Device pairing (short code preferred)
  # The advertised host follows tls.mode: the ACME domain in letsencrypt mode,
  # the mesh IP in selfsigned mode. --host overrides either.
  mcremote pair code --name phone --qr
  mcremote pair code --name phone --host devbox.ts.lallygag.net:7531 --qr   # letsencrypt
  mcremote pair code --name phone --host 100.64.0.1:7531 --qr               # selfsigned only
  mcremote pair code --name phone --ttl 5m
  mcremote pair create --name phone --qr
  mcremote pair create --name phone --qr --host devbox.ts.lallygag.net:7531
  mcremote pair list
  mcremote pair revoke phone

  # Help
  mcremote --help
  mcremote serve --help
  mcremote pair --help
  mcremote setup-service --help
`

const serveExample = `
  mcremote serve
  mcremote serve --listen-host tailscale --listen-port 7531
  mcremote serve --listen-host 127.0.0.1 --listen-port 7531
  mcremote serve --config ~/.config/mcremote/config.yaml
  mcremote serve --config ~/.config/mcremote/config.yaml --log-level debug
  mcremote serve --log-level info --log-format json
  mcremote serve --data-dir ~/.local/share/mcremote
  mcremote serve --listen-host tailscale --listen-port 7531 --data-dir /var/lib/mcremote

  # TLS: Let's Encrypt via ACME DNS-01 (Route 53) — the default once a domain
  # and an email are set. Test against staging first.
  mcremote serve --tls-domain devbox.ts.lallygag.net --tls-email ops@lallygag.net \
    --tls-route53-zone-id Z0123456789ABCDEFGHIJ --tls-route53-region us-east-1 \
    --tls-acme-staging
  mcremote serve --tls-domain devbox.ts.lallygag.net --tls-email ops@lallygag.net \
    --tls-route53-zone-id Z0123456789ABCDEFGHIJ

  # TLS: pinned self-signed (mesh IPs, no public DNS) or plaintext
  mcremote serve --tls-mode selfsigned
  mcremote serve --tls-mode off        # same as --tls=false
`

const pairExample = `
  # Preferred: 8-char code (5 min, one-shot) — type or scan in the phone app
  mcremote pair code --name phone
  mcremote pair code --name phone --qr
  mcremote pair code --name phone --host devbox.ts.lallygag.net:7531 --qr   # letsencrypt
  mcremote pair code --name phone --host 100.64.0.1:7531 --qr               # selfsigned only
  mcremote pair code --name phone --ttl 5m --qr
  mcremote pair          # same as pair code (default subcommand)

  # Long-lived device token (shown once)
  mcremote pair create --name phone
  mcremote pair create --name phone --qr
  mcremote pair create --name phone --qr --host devbox.ts.lallygag.net:7531
  mcremote pair create --name desktop --qr

  # Manage devices
  mcremote pair list
  mcremote pair revoke phone
  mcremote pair revoke <device-id>

  # Custom data dir / config
  mcremote pair code --name phone --data-dir ~/.local/share/mcremote
  mcremote pair list --config ~/.config/mcremote/config.yaml
`

const pairCodeExample = `
  mcremote pair code --name phone
  mcremote pair code --name phone --qr
  mcremote pair code --name phone --host devbox.ts.lallygag.net:7531 --qr   # letsencrypt
  mcremote pair code --name phone --host 100.64.0.1:7531 --qr               # selfsigned only
  mcremote pair code --name phone --ttl 5m
  mcremote pair code --name phone --ttl 10m --qr
  mcremote pair code --name android --host devbox.ts.lallygag.net:7531
  mcremote pair code --name phone --data-dir ~/.local/share/mcremote
`

const pairCreateExample = `
  mcremote pair create --name phone
  mcremote pair create --name phone --qr
  mcremote pair create --name phone --qr --host devbox.ts.lallygag.net:7531
  mcremote pair create --name phone --qr --host 100.64.0.1:7531   # selfsigned only
  mcremote pair create --name desktop --qr
  mcremote pair create --name smoke
  mcremote pair create --name phone --data-dir ~/.local/share/mcremote
`

const pairListExample = `
  mcremote pair list
  mcremote pair list --data-dir ~/.local/share/mcremote
  mcremote pair list --config ~/.config/mcremote/config.yaml
`

const pairRevokeExample = `
  mcremote pair revoke phone
  mcremote pair revoke desktop
  mcremote pair revoke <device-id-or-name>
  mcremote pair revoke phone --data-dir ~/.local/share/mcremote
`

const setupServiceExample = `
  # Install, enable, start, and enable linger (survives logout)
  mcremote setup-service
  mcremote --setup-service
  mcremote setup-service --force

  # Preview unit file without installing
  mcremote setup-service --print-only
  mcremote --setup-service --print-only

  # Bind for mesh / phone clients
  mcremote setup-service --listen-host tailscale --listen-port 7531 --force
  mcremote setup-service --listen-host tailscale --listen-port 7531 --force \
    --config /path/to/configs/config.mesh-grok.yaml

  # Custom config / data / env
  mcremote setup-service --config ~/.config/mcremote/config.yaml --force
  mcremote setup-service --service-config ~/.config/mcremote/config.yaml --force
  mcremote setup-service --data-dir ~/.local/share/mcremote --force
  mcremote setup-service --env MCREMOTE_LOG_LEVEL=debug --force
  mcremote setup-service --env MCREMOTE_LOG_LEVEL=debug --env MCREMOTE_LOG_FORMAT=json --force

  # Point ExecStart at a specific binary (setup-service never copies the binary)
  mcremote setup-service --binary ~/.local/bin/mcremote --force
  mcremote setup-service --binary /usr/local/bin/mcremote --force

  # Skip activation steps
  mcremote setup-service --no-enable --no-start --force
  mcremote setup-service --no-linger --force
  mcremote setup-service --unit-name mcremote --working-directory "$HOME" --force

  # After install
  systemctl --user status mcremote
  journalctl --user -u mcremote -f
  systemctl --user restart mcremote
  systemctl --user disable --now mcremote
`

const versionExample = `
  mcremote version
  mcremote --version
`
