---
title: Limits and security
description: Size caps, timeouts, network boundaries, credentials, and sensitive local data.
---

## Size and time limits

| Area | Limit or behavior |
| --- | --- |
| Capture detail | Initial request and response previews are capped at 256 KiB each and can be expanded progressively |
| Syntax formatting/highlighting | Only complete bodies up to 100 KiB |
| Composer response | At most 2 MiB retained and passed to the GUI |
| Transform file import | At most 1 MiB |
| Transform execution | Five seconds per script by default |
| Relay ingress body | Reverse proxy should allow at least 10 MiB |
| Local API list limit | Defaults to 100 and caps at 1,000 records |

Saving or copying a complete captured body is an explicit full-body operation and can still allocate significant memory.

## Network boundaries

- The interception proxy and local API default to loopback. Keep them there unless exposure is deliberate.
- The local API is unauthenticated and can reveal captured data.
- The desktop opens the relay WebSocket tunnel outbound. It does not require a public desktop port.
- Put the relay behind HTTPS and use WSS for the desktop tunnel.

## Credentials

The relay admin token can register or revoke clients, inspect queued webhooks, and change project ownership. Treat it as a server secret.

Saved relay-admin profiles require the operating-system keyring; Wiretap does not store those tokens as plaintext. Desktop client tokens also prefer the keyring, but can use a mode-`0600` file fallback so headless tunnel startup remains possible.

On Linux, Wiretap follows Secret Service's configured `default` alias rather than creating an application-specific collection.

## Stored traffic

The desktop and relay SQLite databases may contain authorization headers, cookies, personal data, webhook signatures, and complete bodies. Restrict access, encrypt disks and backups where appropriate, and define a retention policy for relay volumes.

Transform programs live in the desktop database. Secrets embedded in code inherit that database's protection.

## Local certificate authority

The interception CA private key enables TLS interception for machines that trust it. Keep the Wiretap data directory private. Prefer shell-scoped trust and install the CA system-wide only when required.

## Release verification

Published SHA-256 checksums can detect corruption when obtained through a trusted channel. Current release artifacts are not cryptographically signed; checksums alone do not prove publisher identity.
