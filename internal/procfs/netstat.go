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
