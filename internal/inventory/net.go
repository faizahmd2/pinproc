package inventory

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// nics lists real interfaces (those backed by a device, plus bonds/bridges that
// carry an address) with link speed from sysfs, as `ip -s link` and `ethtool`
// report them. Container veth pairs and loopback are left out.
func (c *Collector) nics(a, b *sample, secs float64) []NIC {
	base := filepath.Join(c.opt.Sys, "class/net")
	var out []NIC
	for _, name := range readDirNames(base) {
		if name == "lo" || strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "cali") || strings.HasPrefix(name, "flannel") || strings.HasPrefix(name, "cni") {
			continue
		}
		dir := filepath.Join(base, name)
		_, hasDev := os.Stat(filepath.Join(dir, "device"))
		n := NIC{Name: name, State: readString(filepath.Join(dir, "operstate")), MTU: readInt(filepath.Join(dir, "mtu")),
			Duplex: readString(filepath.Join(dir, "duplex"))}
		if s := readInt(filepath.Join(dir, "speed")); s > 0 {
			n.SpeedMbps = s
		}
		if c.opt.Proc == "/proc" {
			if ifi, err := net.InterfaceByName(name); err == nil {
				if addrs, err := ifi.Addrs(); err == nil {
					for _, ad := range addrs {
						if ip, _, err := net.ParseCIDR(ad.String()); err == nil && ip.To4() != nil {
							n.Addrs = append(n.Addrs, ip.String())
						}
					}
				}
			}
		}
		if hasDev != nil && len(n.Addrs) == 0 {
			continue // purely virtual and unaddressed (docker0 with no containers, etc.)
		}
		x, okA := a.nets[name]
		y, okB := b.nets[name]
		if okB {
			n.RxDrops, n.TxDrops, n.RxErrors, n.TxErrors = y.RxDrop, y.TxDrop, y.RxErrs, y.TxErrs
		}
		if okA && okB {
			n.RxBPS = rate(y.RxBytes, x.RxBytes, secs)
			n.TxBPS = rate(y.TxBytes, x.TxBytes, secs)
			n.RxPPS = rate(y.RxPackets, x.RxPackets, secs)
			n.TxPPS = rate(y.TxPackets, x.TxPackets, secs)
			n.DropsPS = rate(y.RxDrop+y.TxDrop, x.RxDrop+x.TxDrop, secs)
		}
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if (len(out[i].Addrs) > 0) != (len(out[j].Addrs) > 0) {
			return len(out[i].Addrs) > 0
		}
		return out[i].RxBPS+out[i].TxBPS > out[j].RxBPS+out[j].TxBPS
	})
	return out
}

func sortShares(s []Share) {
	sort.SliceStable(s, func(i, j int) bool { return s[i].Value > s[j].Value })
}
