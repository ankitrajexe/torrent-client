package main

import (
	"crypto/sha1"
	"log"
	"os"
	"sync/atomic"
)

// resume file layout: [20 bytes info_hash][raw bitfield bytes]
//
// The info_hash prefix lets us catch the case where the user runs a
// different torrent to the same output path — hashes won't match and
// we start fresh instead of mixing data from two different torrents.

// load_resume reads the sidecar file and returns its bitfield.
// Any problem (file missing, wrong size, hash mismatch) is treated as
// "no resume data" — we return a zeroed bitfield and carry on.
func load_resume(resume_path string, info_hash [20]byte, n_pieces int) bitfield {
	bf_len := (n_pieces + 7) / 8
	empty := make(bitfield, bf_len)

	data, err := os.ReadFile(resume_path)
	if os.IsNotExist(err) {
		return empty // first run, nothing to resume
	}
	if err != nil {
		log.Printf("resume: cannot read %s (%v), starting fresh\n", resume_path, err)
		return empty
	}

	if len(data) != 20+bf_len {
		log.Printf("resume: file is wrong size, starting fresh\n")
		return empty
	}

	var stored [20]byte
	copy(stored[:], data[:20])
	if stored != info_hash {
		// different torrent, same output file — don't mix data
		log.Printf("resume: info_hash mismatch (different torrent?), starting fresh\n")
		return empty
	}

	return bitfield(data[20:])
}

// save_resume writes info_hash + bitfield to the sidecar file.
// Called after every verified piece so a Ctrl+C between pieces
// loses at most one piece of progress.
func save_resume(resume_path string, info_hash [20]byte, bf bitfield) error {
	data := make([]byte, 20+len(bf))
	copy(data[:20], info_hash[:])
	copy(data[20:], bf)
	return os.WriteFile(resume_path, data, 0644)
}

// verify_resume checks every bit set in bf by re-hashing the bytes
// already on disk. We never trust the bitfield blindly — a crash mid-write
// can leave partial data that must be re-downloaded.
//
// Bits that fail the hash check are cleared so those pieces go back into
// the work queue. Also seeds dl_stats.bytes_done so the dashboard shows
// the right starting percentage instead of 0%.
func verify_resume(out_file *os.File, tf *torrent_file, bf bitfield) (bitfield, int) {
	done := 0
	for i, expected := range tf.piece_hashes {
		if !bf.has_piece(i) {
			continue
		}
		length := tf.piece_length_at(i)
		buf := make([]byte, length)
		offset := int64(i) * int64(tf.piece_length)
		if _, err := out_file.ReadAt(buf, offset); err != nil {
			bf[i/8] &^= 1 << uint(7-(i%8)) // clear the bit
			log.Printf("resume: piece %d read error (%v), re-downloading\n", i, err)
			continue
		}
		if sha1.Sum(buf) != expected {
			bf[i/8] &^= 1 << uint(7-(i%8)) // clear the bit
			log.Printf("resume: piece %d hash mismatch, re-downloading\n", i)
			continue
		}
		done++
		atomic.AddInt64(&dl_stats.bytes_done, int64(length))
	}
	return bf, done
}
