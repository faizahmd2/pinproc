package netmap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTCP(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "net"), 0755)
	// sl local rem st ... inode(10th field, index 9)
	body := "" +
		"  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 11111 1 0 0\n" +
		"   1: 0100007F:1F90 0200007F:ABCD 01 00000000:00000000 00:00000000 00000000     0        0 22222 1 0 0\n"
	os.WriteFile(filepath.Join(root, "net/tcp"), []byte(body), 0644)
	conns := Connections(root)
	if len(conns) != 2 {
		t.Fatalf("want 2 conns, got %d", len(conns))
	}
	// 0x1F90 = 8080
	if conns[0].LocalPort != 8080 || !conns[0].Listen {
		t.Fatalf("conn0 = %+v, want port 8080 listen", conns[0])
	}
	if conns[1].Listen || conns[1].Inode != 22222 {
		t.Fatalf("conn1 = %+v, want established inode 22222", conns[1])
	}
}

func TestParseInode(t *testing.T) {
	if parseInode("socket:[98765]") != 98765 {
		t.Fatal("inode parse failed")
	}
	if parseInode("pipe:[1]") != 0 {
		t.Fatal("non-socket should be 0")
	}
}
