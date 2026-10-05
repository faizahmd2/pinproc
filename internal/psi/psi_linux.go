//go:build linux

package psi

import (
	"context"
	"fmt"
	"os"
	"syscall"
)

// pressurePath resolves the PSI file that accepts triggers for a resource.
// Modern kernels (e.g. Ubuntu 6.x) accept triggers only on cgroup v2 unified
// pressure files, not on /proc/pressure/*. The cgroup v2 root represents the
// whole machine; a leaf path can be passed to ArmPath for per-service triggers.
// /proc/pressure/* is kept as a fallback for older kernels that honour it.
func pressurePath(res Resource) (string, error) {
	cg := "/sys/fs/cgroup/" + string(res) + ".pressure"
	if _, err := os.Stat(cg); err == nil {
		return cg, nil
	}
	proc := "/proc/pressure/" + string(res)
	if _, err := os.Stat(proc); err == nil {
		return proc, nil
	}
	return "", ErrUnsupported
}

// Available reports whether the kernel exposes PSI pressure files. Whether it
// also accepts *triggers* (and whether this process may write them) is only known
// for certain once Arm succeeds.
func Available() bool {
	if _, err := os.Stat("/sys/fs/cgroup/cpu.pressure"); err == nil {
		return true
	}
	_, err := os.Stat("/proc/pressure/cpu")
	return err == nil
}

// Trigger is an armed PSI trigger. Wait blocks until the kernel signals it.
type Trigger struct {
	res  Resource
	f    *os.File
	epfd int
}

// Arm installs a PSI trigger and returns it ready to Wait on. The returned
// Trigger must be Closed. If the kernel lacks PSI trigger support, Arm returns an
// error wrapping ErrUnsupported so callers can fall back.
func Arm(cfg Config) (*Trigger, error) {
	spec, err := cfg.spec()
	if err != nil {
		return nil, err
	}
	path, err := pressurePath(cfg.Resource)
	if err != nil {
		return nil, err
	}
	// O_RDWR is required: writing the spec arms the trigger; the fd stays open to
	// keep it armed and to be polled.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("psi: open %s: %w", path, err)
	}
	if _, err := f.WriteString(spec); err != nil {
		_ = f.Close()
		// EINVAL/ENOTSUP here means PSI exists but triggers do not (kernel < 5.2
		// or CONFIG_PSI without trigger support).
		return nil, fmt.Errorf("psi: arm %q on %s: %w (%v)", spec, path, ErrUnsupported, err)
	}
	epfd, err := syscall.EpollCreate1(syscall.EPOLL_CLOEXEC)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("psi: epoll_create1: %w", err)
	}
	// PSI triggers notify via POLLPRI, not POLLIN.
	ev := syscall.EpollEvent{Events: syscall.EPOLLPRI, Fd: int32(f.Fd())}
	if err := syscall.EpollCtl(epfd, syscall.EPOLL_CTL_ADD, int(f.Fd()), &ev); err != nil {
		_ = syscall.Close(epfd)
		_ = f.Close()
		return nil, fmt.Errorf("psi: epoll_ctl: %w", err)
	}
	return &Trigger{res: cfg.Resource, f: f, epfd: epfd}, nil
}

// Resource returns the resource this trigger watches.
func (t *Trigger) Resource() Resource { return t.res }

// Wait blocks until the kernel reports the configured stall, the context is
// cancelled, or an error occurs. It returns nil when the trigger fired. The
// context is honoured within one poll tick (<=1s) without needing a self-pipe.
func (t *Trigger) Wait(ctx context.Context) error {
	events := make([]syscall.EpollEvent, 1)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := syscall.EpollWait(t.epfd, events, 1000)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			return fmt.Errorf("psi: epoll_wait: %w", err)
		}
		if n == 0 {
			continue // timeout; re-check context and keep waiting
		}
		if events[0].Events&(syscall.EPOLLERR|syscall.EPOLLHUP) != 0 {
			return fmt.Errorf("psi: trigger fd reported error/hup")
		}
		if events[0].Events&syscall.EPOLLPRI != 0 {
			return nil
		}
		// Any other event: loop and re-evaluate.
	}
}

// Close releases the trigger and its epoll fd.
func (t *Trigger) Close() error {
	if t == nil {
		return nil
	}
	if t.epfd != 0 {
		_ = syscall.Close(t.epfd)
		t.epfd = 0
	}
	if t.f != nil {
		err := t.f.Close()
		t.f = nil
		return err
	}
	return nil
}
