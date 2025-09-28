# learnP2P

A simple, secure peer-to-peer file transfer tool written in Go. It discovers peers on the local network (mDNS) for TCP connections, or pairs interactively via WebRTC. Transfers are end-to-end encrypted using AES-256-GCM with keys exchanged via RSA-OAEP.

---

## What this app does
- Discovers peers on your LAN using mDNS and connects over TCP with a small password-protected handshake.
- Alternatively, pairs interactively over the internet using WebRTC (copy/paste base64 offer/answer) with no mDNS.
- Sends one or more files over a single connection.
- Encrypts each file end-to-end (fresh AES-256 key per file) and binds metadata to content for integrity.

Typical use cases:
- Quickly send files between two machines on the same network without setting up servers or shares.
- When NAT/firewalls prevent direct TCP, use the WebRTC mode to relay via standard STUN/ICE and still reuse the same secure transfer protocol.

---

## Features (at a glance)
- Local discovery: mDNS (zeroconf) shows peers, excludes self.
- TCP handshake: simple HALO/WELCOME-like auth with a shared password.
- WebRTC interactive pairing (Pion) with a data channel adapted to a stream.
- Unified encrypted transfer protocol for both transports.
- Multi-file sessions over the same connection.

---

## How it works

### 1) Discovery and connection
- TCP/mDNS mode:
  - The app advertises itself with a name and port.
  - A REPL lists discovered peers. You pick one and enter its password to connect.
  - The TCP handshake authenticates both sides using a short, plain-text exchange protected by the fact it occurs on a local network and precedes the encrypted file transfer.
- WebRTC mode:
  - Two roles: sender and receiver. The sender creates an OFFER (base64); the receiver pastes it, returns an ANSWER (base64). No mDNS in this mode.
  - A WebRTC data channel is opened and wrapped to behave like a stream, so the same file-transfer logic is reused.

### 2) Secure file transfer protocol
A fresh AES-256-GCM key is used per file. The key is sent securely using the receiver's RSA public key.

Message flow per file:
1. Receiver → Sender (publish RSA public key)
   - 0x01 | uint32(pubLen) | pubDER
   - pubDER is the receiver's RSA-4096 public key encoded in PKIX/DER.
2. Sender → Receiver (key header, version 0x02)
   - 0x02 | uint32(encKeyLen) | encKey | baseNonce
   - encKey is the AES-256 key encrypted with RSA-OAEP (SHA-256) using the receiver's public key.
   - baseNonce is 12 bytes (GCM nonce base) used with an incrementing counter for each message.
3. Sender → Receiver (encrypted manifest)
   - uint32(len) | ciphertext
   - Plaintext is JSON: { name, size, hash } where hash is SHA-256 (hex) of the file.
   - AEAD: AES-256-GCM, nonce derived from baseNonce + counter, AAD = "manifest".
4. Sender → Receiver (encrypted chunks)
   - Repeated: uint32(len) | ciphertext
   - Each chunk is up to 1 MiB before encryption.
   - AEAD: AES-256-GCM, nonce derived from baseNonce + counter, AAD = manifest SHA-256 bytes.

Nonces:
- 12-byte baseNonce is random per file. The last 4 bytes encode a big-endian counter incremented per message (manifest and each chunk).

Integrity binding:
- The manifest is bound with AAD="manifest".
- Data chunks are bound with AAD=manifest's SHA-256, ensuring data is tied to the specific metadata.

Filesystem handling on receive:
- Files are written to `public/` using a temporary `.part` file and then atomically renamed on success.
 - The receiver computes the file's SHA-256 while writing and verifies it equals the manifest hash before renaming. If it doesn't match, the partial file is deleted and the transfer fails.

Notes:
- Backward compatibility is removed; only header version 0x02 (RSA-OAEP) is supported.
- A fresh AES key and nonce base are used for every file.

---

## Install and build
Requirements:
- Go 1.21+ (recommended)

Build:
```powershell
# From the repo root
go build -v
```
This produces `learnP2P.exe` on Windows (or `learnP2P` on other platforms).

---

## Usage

### TCP/mDNS mode (local network)
Receiver and sender both run the app. The receiver should know the password the sender will enter when connecting.

Start the app (listening, advertising via mDNS):
```powershell
.\learnP2P.exe --port 8000 --password hello
```
- The app displays discovered peers and waits for your input.
- By default, if `--password` isn’t provided, the expected password on the receiver is set to the node name.

On the sender machine:
- Wait for the target peer to appear in the list.
- Enter its index.
- When prompted, enter the receiver's password.
- After connecting, send files repeatedly with:
```text
send <path-to-file>
```
The receiver writes files to `public\<filename>`.

### WebRTC mode (interactive pairing)
No mDNS in this mode; use base64 OFFER/ANSWER exchange.

Receiver:
```powershell
.\learnP2P.exe --webrtc-recv
```
- Paste the OFFER from the sender.
- Copy the printed ANSWER back to the sender.

Sender:
```powershell
.\learnP2P.exe --webrtc-send
```
- Copy the printed OFFER to the receiver.
- Paste the receiver’s ANSWER back when prompted.
- Send files repeatedly with:
```text
send <path-to-file>
```
Files are saved under `public\...` on the receiver.

---

## Security model (plain language)
- Confidentiality: AES-256-GCM encrypts manifest and file data. A new AES key is generated for each file.
- Key exchange: The receiver first sends its RSA-4096 public key. The sender encrypts the AES key using RSA-OAEP (SHA-256), so only the receiver can decrypt it.
- Integrity and binding: AES-GCM provides integrity. Additional AAD binds the manifest and chunks, helping detect mismatches or tampering.
   - The receiver performs a SHA-256 checksum verification against the manifest after the transfer completes.
- Nonces: Each encrypted message uses a unique nonce derived from a random base plus a counter, avoiding nonce reuse.

Limitations and recommendations:
- Forward secrecy: Not implemented (the RSA key is long-lived for the process). If long-term key compromise is a concern, consider rotating keys frequently or switching to an ephemeral ECDH design (e.g., X25519 + HKDF).
- Password authentication: TCP connection uses a simple password gating, not a full authentication protocol. For sensitive environments, add stronger authentication.
- Large files: Works in chunks, but resume/retry and progress tracking are not implemented.

---

## Project layout
- `main.go` — CLI, mode selection, mDNS discovery, and connection REPL.
- `connections/` — TCP handshake, mDNS, WebRTC data channel adapter and signaling helpers.
- `transfer/` — Manifest building and secure sender/receiver logic.
- `crypto/` — AES-GCM and RSA-OAEP utilities.

---

## Troubleshooting
- No peers found (TCP/mDNS): Ensure both devices are on the same subnet and mDNS/UDP multicast is allowed by the firewall. Confirm both use the same `--port`.
- Connection fails due to password: Ensure the receiver started with the expected `--password` (or knows its default node name used as password).
- WebRTC pairing stalls: Double-check the OFFER/ANSWER copy-paste. Some terminals wrap long lines; avoid extra whitespace.
- File not appearing: Check the receiver's `public/` directory and the program logs for decryption errors.

---

## Defaults
- Node name: `P2PNode2-<COMPUTERNAME>` on Windows if `--name` is not given.
- Password (TCP receiver): Defaults to the node name if `--password` is not provided.
- Chunk size: 1 MiB per data chunk prior to encryption.

---

## Next steps (ideas)
- Forward secrecy using ephemeral ECDH (X25519 + HKDF) for per-session keys.
- Progress UI and transfer resume.
- Configurable chunk size and timeouts; richer error reporting.
