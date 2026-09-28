package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"
)

// shared_bitfield wraps the done bitfield with a read/write mutex.
// Many seeding goroutines can check has_piece at the same time (RLock),
// while the single download goroutine marks pieces done (Lock).
// Readers never block each other — only a write blocks briefly.
type shared_bitfield struct {
	mu sync.RWMutex
	bf bitfield
}

// open_listener tries ports 6881..6889 and returns the first one that binds.
func open_listener() (net.Listener, int, error) {
	for port := 6881; port <= 6889; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err == nil {
			return ln, port, nil
		}
	}
	return nil, 0, fmt.Errorf("no free port in range 6881-6889")
}

// format_bitfield wraps the bitfield bytes in a msg_bitfield message.
func format_bitfield(bf bitfield) *message {
	payload := make([]byte, len(bf))
	copy(payload, bf)
	return &message{id: msg_bitfield, payload: payload}
}

// format_piece builds a msg_piece message.
// Payload: 4-byte index + 4-byte begin + block data.
func format_piece(index, begin int, data []byte) *message {
	payload := make([]byte, 8+len(data))
	binary.BigEndian.PutUint32(payload[0:4], uint32(index))
	binary.BigEndian.PutUint32(payload[4:8], uint32(begin))
	copy(payload[8:], data)
	return &message{id: msg_piece, payload: payload}
}

// parse_request decodes a msg_request payload: 3 × 4-byte big-endian ints.
// Returns an error if the payload is malformed or the requested block is
// larger than the standard 16 KiB maximum.
func parse_request(msg *message) (index, begin, length int, err error) {
	if msg.id != msg_request {
		return 0, 0, 0, fmt.Errorf("expected request (id %d), got %d", msg_request, msg.id)
	}
	if len(msg.payload) < 12 {
		return 0, 0, 0, fmt.Errorf("request payload too short: %d bytes", len(msg.payload))
	}
	index = int(binary.BigEndian.Uint32(msg.payload[0:4]))
	begin = int(binary.BigEndian.Uint32(msg.payload[4:8]))
	length = int(binary.BigEndian.Uint32(msg.payload[8:12]))
	if length > block_size {
		return 0, 0, 0, fmt.Errorf("request length %d exceeds max %d", length, block_size)
	}
	return index, begin, length, nil
}

// handle_upload_conn serves one incoming peer from handshake to disconnect.
//
// It opens its own read-only file handle so it is completely independent
// of the download goroutine's handle — no file-level locking needed.
func handle_upload_conn(conn net.Conn, tf *torrent_file, peer_id [20]byte, sbf *shared_bitfield, out_path string) {
	defer conn.Close()
	addr := conn.RemoteAddr()

	// each connection gets its own read handle — independent of the downloader
	out_file, err := os.Open(out_path)
	if err != nil {
		log.Printf("seed %s: could not open file: %v\n", addr, err)
		return
	}
	defer out_file.Close()

	// peer has 30s to finish the handshake
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	hs, err := read_handshake(conn)
	if err != nil {
		log.Printf("seed %s: handshake read error: %v\n", addr, err)
		return
	}
	if hs.pstr != "BitTorrent protocol" {
		log.Printf("seed %s: wrong protocol string %q\n", addr, hs.pstr)
		return
	}
	if hs.info_hash != tf.info_hash {
		log.Printf("seed %s: info_hash mismatch, dropping\n", addr)
		return
	}
	log.Printf("seed: incoming connection from %s, handshake ok", conn.RemoteAddr())

	// send our handshake back
	if _, err := conn.Write(new_handshake(tf.info_hash, peer_id).serialize()); err != nil {
		log.Printf("seed %s: handshake write error: %v\n", addr, err)
		return
	}

	// snapshot the bitfield under RLock so the message is consistent
	sbf.mu.RLock()
	bf_msg := format_bitfield(sbf.bf)
	sbf.mu.RUnlock()
	if _, err := conn.Write(bf_msg.serialize()); err != nil {
		log.Printf("seed %s: bitfield write error: %v\n", addr, err)
		return
	}

	log.Printf("seed: peer connected from %s\n", addr)

	// message loop — reset the deadline on every read so idle peers time out
	for {
		conn.SetDeadline(time.Now().Add(2 * time.Minute))
		msg, err := read_message(conn)
		if err != nil {
			log.Printf("seed %s: %v\n", addr, err)
			return
		}
		if msg == nil {
			continue // keep-alive, nothing to do
		}

		switch msg.id {

		case msg_interested:
			unchoke := &message{id: msg_unchoke}
			if _, err := conn.Write(unchoke.serialize()); err != nil {
				log.Printf("seed %s: unchoke write error: %v\n", addr, err)
				return
			}

		case msg_request:
			index, begin, length, err := parse_request(msg)
			if err != nil {
				log.Printf("seed %s: bad request: %v\n", addr, err)
				return
			}

			// never send data for a piece we haven't verified
			sbf.mu.RLock()
			have := sbf.bf.has_piece(index)
			sbf.mu.RUnlock()
			if !have {
				continue // not ready yet, silently skip
			}

			// validate byte range against actual piece size
			piece_len := tf.piece_length_at(index)
			if begin < 0 || length <= 0 || begin+length > piece_len {
				log.Printf("seed %s: request out of bounds (piece %d begin %d len %d)\n",
					addr, index, begin, length)
				return
			}

			buf := make([]byte, length)
			offset := int64(index)*int64(tf.piece_length) + int64(begin)
			if _, err := out_file.ReadAt(buf, offset); err != nil {
				log.Printf("seed %s: ReadAt piece %d: %v\n", addr, index, err)
				return
			}

			piece_msg := format_piece(index, begin, buf)
			if _, err := conn.Write(piece_msg.serialize()); err != nil {
				log.Printf("seed %s: piece write error: %v\n", addr, err)
				return
			}

		case msg_choke, msg_not_interested, msg_cancel:
			// no action needed for a minimal seeder

		default:
			// unknown or unneeded message type, ignored
		}
	}
}

// start_seed_listener accepts connections in a loop and hands each off to
// its own goroutine. Returns when ln is closed (after download finishes).
func start_seed_listener(ln net.Listener, tf *torrent_file, peer_id [20]byte, sbf *shared_bitfield, out_path string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed, exit cleanly
		}
		go handle_upload_conn(conn, tf, peer_id, sbf, out_path)
	}
}
