package procfs

import "testing"

func TestParseMountInfo(t *testing.T) {
	in := []byte("36 29 8:1 / /var/lib/docker rw,relatime - ext4 /dev/sda1 rw\n37 29 0:42 / /run/user/1000 rw,nosuid,nodev - tmpfs tmpfs rw,size=1G\n")
	got, err := ParseMountInfo(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d mounts", len(got))
	}
	if got[0].MountPoint != "/var/lib/docker" || got[0].FSType != "ext4" {
		t.Fatalf("unexpected mount: %+v", got[0])
	}
}
