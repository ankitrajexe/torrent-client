package main

import (
	"fmt"
	"log"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <torrent file> <output file>\n", os.Args[0])
		os.Exit(1)
	}

	torrent_path := os.Args[1]
	output_path := os.Args[2]

	tf, err := open(torrent_path)
	if err != nil {
		log.Fatalf("could not open torrent file: %v\n", err)
	}

	if err := tf.download(output_path); err != nil {
		log.Fatalf("download failed: %v\n", err)
	}

	fmt.Printf("downloaded %s to %s\n", tf.name, output_path)
}
