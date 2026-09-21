package procfs

import (
	"errors"
	"strconv"
	"strings"
)

type MountInfo struct {
	ID, Parent                   int
	Major, Minor                 uint64
	Root, MountPoint             string
	FSOptions                    []string
	FSType, Source, SuperOptions string
}

func ParseMountInfo(b []byte) ([]MountInfo, error) {
	var out []MountInfo
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		sep := -1
		for i, f := range fields {
			if f == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(fields) <= sep+3 {
			continue
		}
		id, e1 := strconv.Atoi(fields[0])
		parent, e2 := strconv.Atoi(fields[1])
		dev := strings.SplitN(fields[2], ":", 2)
		if e1 != nil || e2 != nil || len(dev) != 2 {
			continue
		}
		major, e3 := strconv.ParseUint(dev[0], 10, 64)
		minor, e4 := strconv.ParseUint(dev[1], 10, 64)
		if e3 != nil || e4 != nil {
			continue
		}
		out = append(out, MountInfo{ID: id, Parent: parent, Major: major, Minor: minor, Root: unescapeMount(fields[3]), MountPoint: unescapeMount(fields[4]), FSOptions: strings.Split(fields[5], ","), FSType: fields[sep+1], Source: unescapeMount(fields[sep+2]), SuperOptions: fields[sep+3]})
	}
	if len(out) == 0 {
		return nil, errors.New("no mount records")
	}
	return out, nil
}

func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, "\\").Replace(s)
}
