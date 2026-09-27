package procfs

import "testing"

func TestParseNetSNMP(t *testing.T) {
	in := []byte("Tcp: RtoAlgorithm RtoMin ActiveOpens RetransSegs InErrs OutRsts\nTcp: 1 200 10 12 2 3\n")
	got, err := ParseNetSNMP(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.RetransSegs != 12 || got.ActiveOpens != 10 {
		t.Fatalf("unexpected stats: %+v", got)
	}
}

func TestParseNetStat(t *testing.T) {
	in := []byte("TcpExt: ListenOverflows ListenDrops\nTcpExt: 7 2\n")
	got, err := ParseNetStat(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.ListenOverflows != 7 || got.ListenDrops != 2 {
		t.Fatalf("unexpected stats: %+v", got)
	}
}

func TestParseSockStat(t *testing.T) {
	in := []byte("sockets: used 14\nTCP: inuse 8 orphan 1 tw 3 alloc 10 mem 2\nUDP: inuse 2\n")
	got, err := ParseSockStat(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.SocketsUsed != 14 || got.TCPInUse != 8 || got.TCPOrphan != 1 || got.TCPTimeWait != 3 || got.TCPAlloc != 10 || got.UDPInUse != 2 {
		t.Fatalf("unexpected stats: %+v", got)
	}
}

func TestParseTCPTable(t *testing.T) {
	in := []byte("  sl local_address rem_address st tx_queue rx_queue tr tm->when retrnsmt uid timeout inode ref pointer\n" +
		"   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000  100 0 12345 1 0000000000000000 100 0 0 10 0\n")
	got := ParseTCPTable(in)
	if len(got) != 1 {
		t.Fatalf("entries=%d", len(got))
	}
	if got[0].Inode != 12345 || got[0].State != "LISTEN" || got[0].LocalPort != 8080 {
		t.Fatalf("unexpected entry: %+v", got[0])
	}
}
