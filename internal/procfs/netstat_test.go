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
