package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"golang.org/x/time/rate"
)

func main() {
	maxSpeed := flag.Int("max-speed", 0, "upload speed cap in KB/s (0 = unlimited)")
	flag.Parse()

	if flag.NArg() != 2 {
		fmt.Fprintf(os.Stderr, "usage: %s [flags] <torrent file> <output file>\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}

	torrent_path := flag.Arg(0)
	output_path := flag.Arg(1)

	// Build a shared upload limiter.  Burst must be at least one full block
	// (16 KiB) so WaitN never returns ErrExceedsMaxBurst at any speed cap.
	var ul_lim *rate.Limiter
	if *maxSpeed > 0 {
		bps := rate.Limit(*maxSpeed * 1024)
		burst := *maxSpeed * 1024
		if burst < block_size {
			burst = block_size
		}
		ul_lim = rate.NewLimiter(bps, burst)
	}

	tf, err := open(torrent_path)
	if err != nil {
		log.Fatalf("could not open torrent file: %v\n", err)
	}

	if err := tf.download(output_path, ul_lim); err != nil {
		log.Fatalf("download failed: %v\n", err)
	}

	fmt.Printf("downloaded %s to %s\n", tf.name, output_path)
}
