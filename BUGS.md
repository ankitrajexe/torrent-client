# Bug Tracker

Bugs found and fixed across the codebase. Each entry has the file, the function, what was wrong, and the fix.

---

## torrent.go

### `hash()` — pointer passed to Marshal instead of value

`bencode.Marshal` was getting a pointer to the struct. The library doesn't encode pointers — it encodes values. So the bencoded output came out wrong, the SHA1 was computed over garbage, and the info hash was wrong. Every handshake fails because no peer recognizes it.

```go
// wrong — pointer, library skips it
bencode.Marshal(&buf, i)

// fix — pass the value
bencode.Marshal(&buf, *i)
```

**Status: fixed**

---

## wire.go

### `has_piece()` — shift goes the wrong way

BitTorrent bitfields are big-endian bit order, so bit 0 of a piece is the MSB of its byte. The code shifts right by `offset` which reads from the LSB side. The fix is the same thing `set_piece` already does — subtract from 7.

```go
// wrong
return bf[byte_index] >> uint(offset) & 1 != 0

// fix
return bf[byte_index] >> uint(7-offset) & 1 != 0
```

**Status: not applied**

---

### `format_request()` — little-endian instead of big-endian

Every integer on the BitTorrent wire is big-endian. `format_request` uses `LittleEndian` for all three fields. Every other function in this file uses `BigEndian` correctly — this one was just wrong.

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

**Status: not applied**

---

### `read_handshake()` — buffer is one byte short

After reading `pstrlen`, the remaining bytes to read are: pstr + 8 reserved + 20 info_hash + 20 peer_id = `pstr_len + 49`. The code allocates `pstr_len + 48`, so the last byte of peer_id is never pulled off the wire.

```go
// wrong
buf := make([]byte, pstr_len+48)

// fix
buf := make([]byte, pstr_len+49)
```

**Status: not applied**

---

## p2p.go

### `handle_message()` — backlog never goes down

`fill_requests` stops queuing new block requests once `pp.backlog` hits `max_backlog` (5). But when a piece block arrives, `pp.backlog` was never decremented, so after the first 5 requests go out, no more ever get sent and the download just stalls.

```go
// wrong — backlog only ever goes up
pp.downloaded += n

// fix — a piece came in, that slot is free now
pp.downloaded += n
pp.backlog--
```

**Status: fixed**