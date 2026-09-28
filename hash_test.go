package main

import (
	"fmt"
	"testing"
)

func TestHash(t *testing.T) {
	tf, err := open("debian.torrent")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("hash: %x\n", tf.info_hash)
	fmt.Println("announce:", tf.announce)

	id, _ := new_peer_id()
	peers, err := tf.request_peers(id, 6881)
	fmt.Println("peers:", len(peers), "err:", err)
}