# BitTorrent Client 

A concurrent BitTorrent client written in Go implementing the core BitTorrent Peer Protocol (BEP 0003).

> **Note to Participants:**
> This codebase is intentionally incomplete and contains implementation defects. Please consult the **Problem Statement** document for your submission guidelines and evaluation criteria.

## Features (Baseline Framework)

* `.torrent` metainfo file parsing via `bencode-go`
* HTTP Tracker Announce client
* Concurrent piece downloading across multiple peers using worker goroutines and channels
* Block pipelining 16 KB chunk requests and SHA-1 piece verification

## BitTorrent Protocol Overview

1. **Metainfo (.torrent):** Parses piece length, file size, and the SHA-1 piece hash string.
2. **Tracker Announce:** Contacts the tracker over HTTP to request active peer `IP:Port` combinations.
3. **Peer Handshake:** Connects to peers over TCP and exchanges protocol handshakes.
4. **Piece Transfer:** Sends `interested`, receives `unchoke`, and pipelines 16 KB block requests using `request` messages (`index`, `begin`, `length`).

## Getting Started

### Prerequisites

* **Go 1.20+** installed on your system.

### Building & Running

1. **Clone the repository:**
```bash
git clone https://github.com/TatHack-Tathva/torrent-client.git
cd torrent-client

```

2. **Download dependencies:**
```bash
go mod download

```

3. **Build the binary:**
```bash
go build -o torrent-client .

```

4. **Run against a sample torrent file:**
```bash
./torrent-client sample.torrent output.bin

```

## AI Usage Disclosure

We used AI tools (Anthropic Claude for debugging guidance and planning, Google Antigravity/Gemini for writing code) throughout this project, as allowed under Rule 06.

**How we used it:**
- Debugging: Claude helped us understand where to look for bugs (opcode/protocol logic, timing, wire format) based on the symptoms we saw when testing. We ran every test ourselves and verified each fix by re-running the client against real torrents and checking file hashes against official checksums.
- Feature implementation: Antigravity (Gemini) wrote the initial code for the live dashboard, resume support, and seeding, based on requirements and design decisions (e.g. mutex strategy for shared bitfield state, re-verification on resume) that we specified in the prompts. We reviewed every diff before accepting it.
- We did not use AI to write the demo video, and every commit was made from this machine by a registered team member.

**What we can explain:** every bug fix, the resume file format, the shared-bitfield locking strategy, and the wire protocol changes are documented in BUGS.md and we're prepared to walk through any part of the code during technical defense.

We confirmed with organizers that this level of AI use is acceptable as long as we can explain and defend the code, which we can.