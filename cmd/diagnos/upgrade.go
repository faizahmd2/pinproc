package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const installScript = "curl -fsSL https://faizahmd2.github.io/pinproc-apt/install.sh | sudo bash"

// runUpgrade updates an APT-installed pinproc in place: it refreshes only the
// pinproc repository (fast, no full `apt update`) and upgrades just this package.
// Config in /etc/pinproc and data in /var/lib/pinproc are kept; the package's
// postinstall restarts the service.
func runUpgrade(w io.Writer) error {
	if runtime.GOOS != "linux" {
		return errors.New("--upgrade is for Linux installs")
	}
	if os.Geteuid() != 0 {
		return errors.New("upgrading needs root: sudo pinproc --upgrade")
	}
	before, err := installedVersion()
	if err != nil {
		return fmt.Errorf("pinproc was not installed from the APT repository (%v). Install it with:\n  %s", err, installScript)
	}
	fmt.Fprintf(w, "installed: pinproc %s\n", before)

	update := []string{"update", "-q"}
	if list := pinprocSourceList(); list != "" {
		update = append(update, "-o", "Dir::Etc::sourcelist="+list, "-o", "Dir::Etc::sourceparts=-", "-o", "APT::Get::List-Cleanup=0")
		fmt.Fprintf(w, "refreshing %s…\n", list)
	} else {
		fmt.Fprintln(w, "refreshing package lists…")
	}
	if err := aptGet(w, update...); err != nil {
		return err
	}
	if err := aptGet(w, "install", "--only-upgrade", "-y", "-q", "pinproc"); err != nil {
		return err
	}
	after, _ := installedVersion()
	if after == before {
		fmt.Fprintf(w, "pinproc %s is already the latest version.\n", after)
		return nil
	}
	fmt.Fprintf(w, "upgraded: pinproc %s → %s (config and data kept, service restarted)\n", before, after)
	return nil
}

func installedVersion() (string, error) {
	out, err := exec.Command("dpkg-query", "-W", "-f=${Status} ${Version}", "pinproc").Output()
	if err != nil {
		return "", errors.New("dpkg has no pinproc package")
	}
	f := strings.Fields(string(out))
	if len(f) < 4 || f[2] != "installed" {
		return "", errors.New("pinproc package is not fully installed")
	}
	return f[3], nil
}

// pinprocSourceList finds the one-line APT source that serves pinproc, so the
// refresh touches only that repository. deb822 .sources files fall back to a
// normal `apt-get update`.
func pinprocSourceList() string {
	files, _ := filepath.Glob("/etc/apt/sources.list.d/*.list")
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(fh)
		found := false
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "#") && strings.Contains(line, "pinproc") {
				found = true
				break
			}
		}
		fh.Close()
		if found {
			return f
		}
	}
	return ""
}

func aptGet(w io.Writer, args ...string) error {
	cmd := exec.Command("apt-get", args...)
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive", "NEEDRESTART_SUSPEND=1")
	cmd.Stdout, cmd.Stderr = w, w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("apt-get %s: %w", strings.Join(args, " "), err)
	}
	return nil
}
