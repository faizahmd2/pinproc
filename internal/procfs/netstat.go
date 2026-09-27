package procfs

import (
	"bufio"
	"bytes"
	"errors"
	"strconv"
	"strings"
)

type TCPStat struct {
	ActiveOpens, PassiveOpens, AttemptFails, EstabResets, CurrEstab uint64
	RetransSegs, InErrs, OutRsts                                    uint64
}

type TCPExt struct {
	ListenOverflows, ListenDrops uint64
}

func ParseNetSNMP(b []byte) (TCPStat, error) {
	fields, err := protoFields(b, "Tcp:")
	if err != nil {
		return TCPStat{}, err
	}
	return TCPStat{
		ActiveOpens: fields["ActiveOpens"], PassiveOpens: fields["PassiveOpens"],
		AttemptFails: fields["AttemptFails"], EstabResets: fields["EstabResets"],
		CurrEstab: fields["CurrEstab"], RetransSegs: fields["RetransSegs"],
		InErrs: fields["InErrs"], OutRsts: fields["OutRsts"],
	}, nil
}

func ParseNetStat(b []byte) (TCPExt, error) {
	fields, err := protoFields(b, "TcpExt:")
	if err != nil {
		return TCPExt{}, err
	}
	return TCPExt{ListenOverflows: fields["ListenOverflows"], ListenDrops: fields["ListenDrops"]}, nil
}

func protoFields(b []byte, proto string) (map[string]uint64, error) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		header := sc.Text()
		if !strings.HasPrefix(header, proto) {
			continue
		}
		if !sc.Scan() {
			break
		}
		values := sc.Text()
		names := strings.Fields(header)[1:]
		vals := strings.Fields(values)[1:]
		out := make(map[string]uint64, len(names))
		for i, name := range names {
			if i >= len(vals) {
				break
			}
			v, _ := strconv.ParseUint(vals[i], 10, 64)
			out[name] = v
		}
		return out, nil
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("section not found: " + proto)
}

type SockStat struct {
	SocketsUsed uint64
	TCPInUse    uint64
	TCPOrphan   uint64
	TCPTimeWait uint64
	TCPAlloc    uint64
	TCPMemory   uint64
	UDPInUse    uint64
}

func ParseSockStat(b []byte) (SockStat, error) {
	var out SockStat
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "sockets:":
			out.SocketsUsed = parseNamedValue(fields[1:], "used")
		case "TCP:":
			out.TCPInUse = parseNamedValue(fields[1:], "inuse")
			out.TCPOrphan = parseNamedValue(fields[1:], "orphan")
			out.TCPTimeWait = parseNamedValue(fields[1:], "tw")
			out.TCPAlloc = parseNamedValue(fields[1:], "alloc")
			out.TCPMemory = parseNamedValue(fields[1:], "mem")
		case "UDP:":
			out.UDPInUse = parseNamedValue(fields[1:], "inuse")
		}
	}
	if out.SocketsUsed == 0 && out.TCPInUse == 0 && out.UDPInUse == 0 {
		return out, errors.New("socket statistics not found")
	}
	return out, nil
}

func parseNamedValue(fields []string, name string) uint64 {
	for i := 0; i+1 < len(fields); i += 2 {
		if fields[i] != name {
			continue
		}
		v, _ := strconv.ParseUint(fields[i+1], 10, 64)
		return v
	}
	return 0
}

type TCPEntry struct {
	Inode      uint64
	State      string
	LocalPort  int
	RemotePort int
}

func ParseTCPTable(b []byte) []TCPEntry {
	var out []TCPEntry
	states := map[string]string{
		"01": "ESTABLISHED",
		"02": "SYN_SENT",
		"03": "SYN_RECV",
		"04": "FIN_WAIT1",
		"05": "FIN_WAIT2",
		"06": "TIME_WAIT",
		"07": "CLOSE",
		"08": "CLOSE_WAIT",
		"09": "LAST_ACK",
		"0A": "LISTEN",
		"0B": "CLOSING",
		"0C": "NEW_SYN_RECV",
	}
	for _, line := range strings.Split(string(b), "\n")[1:] {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		local := strings.Split(fields[1], ":")
		remote := strings.Split(fields[2], ":")
		if len(local) != 2 || len(remote) != 2 {
			continue
		}
		lp, err1 := strconv.ParseUint(local[1], 16, 16)
		rp, err2 := strconv.ParseUint(remote[1], 16, 16)
		inode, err3 := strconv.ParseUint(fields[9], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		state := strings.ToUpper(fields[3])
		if name, ok := states[state]; ok {
			state = name
		}
		out = append(out, TCPEntry{Inode: inode, State: state, LocalPort: int(lp), RemotePort: int(rp)})
	}
	return out
}
