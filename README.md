# BitTorrent Client 

A concurrent BitTorrent client written in Go implementing the core BitTorrent Peer Protocol (BEP 0003).

## Features

* `.torrent` metainfo parsing via `bencode-go`, HTTP tracker announce
* Concurrent piece downloading across multiple peers (worker goroutines + channels), block pipelining, SHA-1 piece verification
* **Live terminal dashboard** — real-time download speed, ETA, active peers, progress bar
* **Download resume** — pieces already verified on disk are re-hashed and skipped on restart, only missing pieces are re-requested
* **Seeding** — after download completes, the client keeps a TCP listener open and serves verified pieces to incoming peers (handshake validation, bitfield exchange, request/piece handling)
* **Bandwidth rate limiting** — `-max-speed` flag caps outgoing upload bandwidth (KB/s) using a shared token bucket across all peer connections

All bugs found in the original scaffold and their fixes are documented in [BUGS.md](./BUGS.md).

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

5. **Run with upload bandwidth capped (optional):**
```bash
./torrent-client -max-speed 500 sample.torrent output.bin
```

## AI Usage Disclosure

We used AI tools (Anthropic Claude for debugging guidance and planning, Google Antigravity/Gemini for writing code) throughout this project, as allowed under Rule 06.

**How we used it:**
- Debugging: Claude helped us understand where to look for bugs (info-hash generation, wire protocol serialization, bitfield bit order, backlog flow control) based on the symptoms we saw when testing.
- Feature implementation: Antigravity (Gemini) wrote the initial code for the live dashboard, resume support, seeding, and rate limiting, based on requirements and design decisions (e.g. mutex strategy for shared bitfield state, re-verification on resume) that we specified in the prompts. We reviewed every diff before accepting it.
- We did not use AI to write the demo video, and every commit was made from this machine by a registered team member.

**What we can explain:** every bug fix, the resume file format, the shared-bitfield locking strategy, and the wire protocol changes are documented in BUGS.md and we're prepared to walk through any part of the code during technical defense.

We confirmed with organizers that this level of AI use is acceptable as long as we can explain and defend the code, which we can.