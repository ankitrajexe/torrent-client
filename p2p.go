package main

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const block_size = 16384
const max_backlog = 5

// stats holds counters updated atomically by worker goroutines.
// total_bytes is set once before the download starts and never written again.
type stats struct {
	bytes_done   int64
	active_peers int64
	total_bytes  int64
}

var dl_stats stats

// start_dashboard prints a single updating line to the terminal every 500ms.
// Speed is computed over a rolling ~1.5-second window (3 samples) so it
// reacts quickly instead of showing a lifetime average.
// The line is padded to 70 chars before the carriage return so leftover
// characters from a longer previous render don't bleed through on Windows.
func start_dashboard(done <-chan struct{}) {
	type sample struct {
		t time.Time
		b int64
	}
	var ring [3]sample
	pos := 0

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case now := <-ticker.C:
			done_bytes := atomic.LoadInt64(&dl_stats.bytes_done)
			total := atomic.LoadInt64(&dl_stats.total_bytes)
			peers := atomic.LoadInt64(&dl_stats.active_peers)

			// keep a rolling window; oldest is the slot we're about to overwrite
			oldest := ring[(pos+1)%3]
			ring[pos] = sample{now, done_bytes}
			pos = (pos + 1) % 3

			var speed float64
			if !oldest.t.IsZero() {
				dt := now.Sub(oldest.t).Seconds()
				if dt > 0 {
					speed = float64(done_bytes-oldest.b) / dt
				}
			}

			var pct float64
			if total > 0 {
				pct = float64(done_bytes) / float64(total) * 100
			}

			eta := "??:??"
			if speed > 0 && total > done_bytes {
				secs := int(float64(total-done_bytes) / speed)
				eta = fmt.Sprintf("%d:%02d:%02d", secs/3600, (secs%3600)/60, secs%60)
			}

			var speed_str string
			switch {
			case speed >= 1024*1024:
				speed_str = fmt.Sprintf("%.1f MB/s", speed/1024/1024)
			case speed >= 1024:
				speed_str = fmt.Sprintf("%.1f KB/s", speed/1024)
			default:
				speed_str = fmt.Sprintf("%.0f B/s", speed)
			}

			// 20-char progress bar
			filled := int(pct / 5)
			if filled > 20 {
				filled = 20
			}
			bar := strings.Repeat("=", filled) + strings.Repeat("-", 20-filled)

			line := fmt.Sprintf("[%s] %5.1f%% | %9s | ETA %-9s | %d peers",
				bar, pct, speed_str, eta, peers)

			// pad to a fixed 70 chars so shorter renders don't leave ghost text
			if len(line) < 70 {
				line += strings.Repeat(" ", 70-len(line))
			}
			fmt.Printf("\r%s", line)
		}
	}
}

type piece_work struct {
	index  int
	hash   [20]byte
	length int
}

type piece_result struct {
	index int
	data  []byte
}

type piece_progress struct {
	index      int
	buf        []byte
	downloaded int
	requested  int
	backlog    int
}

func (pp *piece_progress) fill_requests(c *client, piece_length int) error {
	for pp.backlog < max_backlog && pp.requested < piece_length {
		block_len := block_size
		if piece_length-pp.requested < block_size {
			block_len = piece_length - pp.requested
		}
		if err := c.send_request(pp.index, pp.requested, block_len); err != nil {
			return err
		}
		pp.backlog++
		pp.requested += block_len
	}
	return nil
}

func (pp *piece_progress) handle_message(c *client) error {
	msg, err := c.read()
	if err != nil {
		return err
	}
	if msg == nil {
		return nil
	}

	switch msg.id {

	case msg_unchoke:
		c.choked = false
		log.Printf("unchoked by peer\n")

	case msg_choke:
		c.choked = true

	case msg_have:
		index, err := parse_have(msg)
		if err != nil {
			return err
		}
		c.bitfield.set_piece(index)

	case msg_piece:
		n, err := parse_piece(pp.index, pp.buf, msg)
		if err != nil {
			return err
		}
		pp.downloaded += n
		pp.backlog-- // one request fulfilled; allow fill_requests to queue another
	}

	return nil
}

func download_piece(c *client, pw *piece_work) ([]byte, error) {
	pp := &piece_progress{
		index: pw.index,
		buf:   make([]byte, pw.length),
	}

	c.conn.SetDeadline(time.Now().Add(30 * time.Second))
	defer c.conn.SetDeadline(time.Time{})

	for pp.downloaded < pw.length {
		if !c.choked {
			if err := pp.fill_requests(c, pw.length); err != nil {
				return nil, err
			}
		}
		if err := pp.handle_message(c); err != nil {
			return nil, err
		}
	}

	return pp.buf, nil
}

func check_integrity(pw *piece_work, data []byte) error {
	hash := sha1.Sum(data)
	if !bytes.Equal(hash[:], pw.hash[:]) {
		return fmt.Errorf("piece %d failed integrity check", pw.index)
	}
	return nil
}

func start_download_worker(p peer, info_hash [20]byte, peer_id [20]byte, work_ch chan *piece_work, results_ch chan *piece_result) {
	c, err := new_client(p, info_hash, peer_id, len(work_ch))
	if err != nil {
		log.Printf("could not connect to peer %s: %v\n", p, err)
		return
	}
	defer c.conn.Close()
	atomic.AddInt64(&dl_stats.active_peers, 1)
	defer atomic.AddInt64(&dl_stats.active_peers, -1)

	log.Printf("connected to peer %s\n", p)

	if err := c.send_unchoke(); err != nil {
		return
	}
	if err := c.send_interested(); err != nil {
		return
	}
	log.Printf("sent interested to %s, choked=%v\n", p, c.choked)

	for pw := range work_ch {
		if !c.bitfield.has_piece(pw.index) {
			work_ch <- pw
			continue
		}

		data, err := download_piece(c, pw)
		if err != nil {
			work_ch <- pw
			log.Printf("failed to download piece %d from %s: %v\n", pw.index, p, err)
			return
		}

		if err := check_integrity(pw, data); err != nil {
			work_ch <- pw
			log.Printf("piece %d from %s failed integrity check\n", pw.index, p)
			continue
		}

		results_ch <- &piece_result{pw.index, data}
	}
}

func (t *torrent_file) download(out_path string) error {
	log.Println("starting download for", t.name)

	peer_id, err := new_peer_id()
	if err != nil {
		return err
	}

	peers, err := t.request_peers(peer_id, 6881)
	if err != nil {
		return err
	}
	log.Printf("got %d peers from tracker\n", len(peers))

	// open or create the output file; pieces are written in place via WriteAt
	out_file, err := os.OpenFile(out_path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer out_file.Close()

	// pre-allocate on a brand-new file so WriteAt has somewhere to land
	info, err := out_file.Stat()
	if err != nil {
		return err
	}
	if info.Size() == 0 {
		if err := out_file.Truncate(int64(t.length)); err != nil {
			return err
		}
	}

	resume_path := out_path + ".resume"
	done_bf := load_resume(resume_path, t.info_hash, len(t.piece_hashes))
	done_bf, done_pieces := verify_resume(out_file, t, done_bf)
	if done_pieces > 0 {
		log.Printf("resume: %d/%d pieces already verified, skipping them\n",
			done_pieces, len(t.piece_hashes))
	}

	// sbf is the shared view of the bitfield that seeding goroutines read under
	// a lock. It starts as a copy of done_bf so peers see already-done pieces.
	sbf := &shared_bitfield{bf: make(bitfield, len(done_bf))}
	copy(sbf.bf, done_bf)

	ln, seed_port, err := open_listener()
	if err != nil {
		log.Printf("seed: no listener (%v), seeding disabled\n", err)
	} else {
		log.Printf("seeding: listening for peers on port %d", seed_port)
		go start_seed_listener(ln, t, peer_id, sbf, out_path)
	}

	atomic.StoreInt64(&dl_stats.total_bytes, int64(t.length))
	done_dash := make(chan struct{})
	go start_dashboard(done_dash)

	work_ch := make(chan *piece_work, len(t.piece_hashes))
	results_ch := make(chan *piece_result)

	for index, hash := range t.piece_hashes {
		if done_bf.has_piece(index) {
			continue // already verified on disk, skip it
		}
		length := t.piece_length_at(index)
		work_ch <- &piece_work{index, hash, length}
	}

	for _, p := range peers {
		go start_download_worker(p, t.info_hash, peer_id, work_ch, results_ch)
	}

	for done_pieces < len(t.piece_hashes) {
		result := <-results_ch

		begin := int64(result.index) * int64(t.piece_length)
		if _, err := out_file.WriteAt(result.data, begin); err != nil {
			return fmt.Errorf("write piece %d: %w", result.index, err)
		}

		done_bf.set_piece(result.index)
		sbf.mu.Lock()
		sbf.bf.set_piece(result.index)
		sbf.mu.Unlock()
		if err := save_resume(resume_path, t.info_hash, done_bf); err != nil {
			log.Printf("resume: could not save %s: %v\n", resume_path, err)
		}

		atomic.AddInt64(&dl_stats.bytes_done, int64(len(result.data)))
		done_pieces++
	}
	close(work_ch)
	close(done_dash)
	fmt.Printf("\r%-70s\n", "[====================] 100.0% | download complete")
	fmt.Println() // move cursor to a fresh line below the dashboard

	if ln != nil {
		// keep seeding until the user hits Ctrl+C
		fmt.Println("download complete, seeding until Ctrl+C")
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit
		ln.Close() // unblocks start_seed_listener's Accept loop
		fmt.Println()
	}

	return nil
}

func (t *torrent_file) piece_length_at(index int) int {
	begin := index * t.piece_length
	end := begin + t.piece_length
	if end > t.length {
		end = t.length
	}
	return end - begin
}
