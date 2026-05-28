# Zowsup-Go Roadmap

Tracks the milestone-by-milestone rewrite of [`zowsup-cli`](../README.md)
into pure Go. Each milestone is one (or a small handful of) PR(s).

## Milestones

| Milestone | Goal | Status |
|---|---|---|
| **M0** | Foundation: skeleton, sqlite, noise handshake, binxmpp codec, per-account proxy transport, account manager scaffolding, REST/WS skeleton, frontend skeleton | **in progress** (this PR) |
| **M1** | Companion login (QR + pair code) end-to-end → an account reaches "logged in" | pending |
| **M2** | `msg.send` / `msg.sendmedia` + inbound message reception | pending |
| **M3** | `contact.*` family | pending |
| **M4** | `account.getname` / `account.setname` / `account.getavatar` / `account.setavatar` | pending |
| **M5** | Axolotl group fan-out, `md.devices`, `md.remove` | pending |
| **M6** | Registration: `account.setemail` / `verifyemail` / `verifyemailcode` / `set2fa` | pending |
| **M7** | Primary-device pairing: `md.link` / `md.inputcode` (hardest, last) | pending |
| **M8** | `msg.sendad` / `msg.edit` / `msg.revoke` / `msg.quotedreply` | pending |

## Command coverage

Subset of commands listed in the project brief.

### Account
| Command | Milestone | Status |
|---|---|---|
| `account.init` | M1 | not started |
| `account.info` | M1 | not started |
| `account.getname` / `account.setname` | M4 | not started |
| `account.getavatar` / `account.setavatar` | M4 | not started |
| `account.getemail` / `account.setemail` | M6 | not started |
| `account.verifyemail` / `account.verifyemailcode` | M6 | not started |
| `account.set2fa` | M6 | not started |

### Contacts
| Command | Milestone | Status |
|---|---|---|
| `contact.list` | M3 | not started |
| `contact.sync` | M3 | not started |
| `contact.getprofile` | M3 | not started |
| `contact.getavatar` | M3 | not started |
| `contact.getdevices` | M3 | not started |
| `contact.trust` | M3 | not started |

### Messaging
| Command | Milestone | Status |
|---|---|---|
| `msg.send` | M2 | not started |
| `msg.sendmedia` | M2 | not started |
| `msg.sendad` | M8 | not started |
| `msg.quotedreply` | M8 | not started |
| `msg.edit` | M8 | not started |
| `msg.revoke` | M8 | not started |

### Multi-device
| Command | Milestone | Status |
|---|---|---|
| `md.devices` | M5 | not started |
| `md.link` | M7 | not started |
| `md.inputcode` | M7 | not started |
| `md.remove` | M5 | not started |

## Layer coverage (protocol plumbing)

| Layer | Status |
|---|---|
| Noise XX (WA variant) handshake | **M0 done** (handshake state machine + AEAD; no real socket dial yet) |
| Binary XMPP node codec | **M0 done** (full token dictionary, encode/decode, tests) |
| `net.Conn` over web socket frames (`segments`) | M1 |
| Auth (client payload, registration request) | M1 |
| Axolotl ratchet | M5 |
| Message protobuf assembly | M2 |
| Media upload (mmg.whatsapp.net) | M2 |
| App-state push (LTHash) — used by setname, addressbook sync | M4 / M3 |
| Pairing (companion side) | M1 |
| Pairing (primary side, `md.link`/`md.inputcode`) | M7 |
| Registration HTTPS (`v.whatsapp.net`) | M6 |
