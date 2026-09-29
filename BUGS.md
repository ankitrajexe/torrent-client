# BUGS.md

Bugs I found and fixed in this project. Writing this down so I remember what was broken and why.

---

## torrent.go

### `hash()` — was passing a pointer to Marshal, not the value

The `bencode.Marshal` call was getting `i` (a pointer) instead of `*i` (the actual struct). The library doesn't know how to encode a pointer to a struct, so the bencoded output was wrong, the SHA1 came out wrong, and the info hash was garbage. Every single handshake failed silently because no peer ever recognized our hash.

```go
// wrong
bencode.Marshal(&buf, i)

// fix
bencode.Marshal(&buf, *i)
```

**Fixed.**

---

## wire.go

### `has_piece()` — shift direction was backwards

BitTorrent bitfields use big-endian bit order — piece 0 is the most significant bit of byte 0. The code was shifting right by `offset` (0–7), which reads from the LSB side. `set_piece` was already doing it right with `7-offset`. `has_piece` just wasn't consistent with it.

```go
// wrong — reads LSB side
return bf[byte_index] >> uint(offset) & 1 != 0

// fix — same as set_piece
return bf[byte_index] >> uint(7-offset) & 1 != 0
```

**Fixed.**

---

### `format_request()` — used LittleEndian instead of BigEndian

The BitTorrent protocol sends every integer in big-endian (network byte order). `format_request` was using `binary.LittleEndian` for all three fields — index, begin, and length. Every other function in the file used `BigEndian`. Peers were getting completely wrong block coordinates.

```go
// wrong
binary.LittleEndian.PutUint32(payload[0:4], uint32(index))
binary.LittleEndian.PutUint32(payload[4:8], uint32(begin))
binary.LittleEndian.PutUint32(payload[8:12], uint32(length))

// fix
binary.BigEndian.PutUint32(payload[0:4], uint32(index))
binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
binary.BigEndian.PutUint32(payload[8:12], uint32(length))
```

**Fixed.**

---

### `read_handshake()` — buffer was 1 byte too short

After reading the 1-byte `pstrlen`, the rest of the handshake is: pstr + 8 reserved bytes + 20 info_hash + 20 peer_id = `pstr_len + 49` bytes. The buffer was allocated as `pstr_len + 48`, so the last byte of `peer_id` was never read off the wire. Subtle because it doesn't crash — it just silently truncates peer_id.

```go
// wrong
buf := make([]byte, pstr_len+48)

// fix
buf := make([]byte, pstr_len+49)
```

**Fixed.**

---

## p2p.go

### `handle_message()` — backlog counter never went down

`fill_requests` sends block requests up to `max_backlog` (5) in flight at a time. It checks `pp.backlog < max_backlog` before sending each one. The problem was that when a piece block came back, `pp.backlog` was never decremented. So after the first 5 requests went out, `backlog` stayed at 5 forever, `fill_requests` never sent anything again, and the download just stalled waiting for blocks that would never come.

```go
// wrong
pp.downloaded += n

// fix
pp.downloaded += n
pp.backlog--
```

**Fixed.**
