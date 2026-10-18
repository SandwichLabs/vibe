package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

type ifreq struct {
	name  [16]byte
	flags uint16
	_     [22]byte
}

func loUp() error {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	var r ifreq
	copy(r.name[:], "lo")
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.SIOCGIFFLAGS, uintptr(unsafe.Pointer(&r))); e != 0 {
		return e
	}
	r.flags |= syscall.IFF_UP | syscall.IFF_RUNNING
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.SIOCSIFFLAGS, uintptr(unsafe.Pointer(&r))); e != 0 {
		return e
	}
	return nil
}

// setupInNS runs inside the helper's own user+net+mount namespaces, before GTK:
// brings lo up, starts the in-namespace instruments (listener, DNS, fake resolved).
func setupInNS(c helperCfg) error {
	if err := loUp(); err != nil {
		return fmt.Errorf("lo up: %w", err)
	}
	h, err := newHitLog(c.listen, "INNS")
	if err != nil {
		return err
	}
	// c.target holds the port to listen on, in-namespace
	host, port := "127.0.0.1", c.target[len("127.0.0.1:"):]
	if _, err := startListeners(host+":"+port, h); err != nil {
		return fmt.Errorf("inns listeners: %w", err)
	}
	if err := syscall.Mount("", "/", "", syscall.MS_REC|syscall.MS_PRIVATE, ""); err != nil {
		return fmt.Errorf("make-rprivate: %w", err)
	}
	switch c.dns {
	case "udp53":
		dir, _ := os.MkdirTemp("", "ns-dns-")
		rc := filepath.Join(dir, "resolv.conf")
		ns := filepath.Join(dir, "nsswitch.conf")
		os.WriteFile(rc, []byte("nameserver 127.0.0.1\n"), 0o644)
		os.WriteFile(ns, []byte("hosts: dns\n"), 0o644)
		if err := dnsServer("127.0.0.1:53", h); err != nil {
			return fmt.Errorf("dns server: %w", err)
		}
		if err := syscall.Mount(rc, "/etc/resolv.conf", "", syscall.MS_BIND, ""); err != nil {
			return fmt.Errorf("bind resolv.conf: %w", err)
		}
		if err := syscall.Mount(ns, "/etc/nsswitch.conf", "", syscall.MS_BIND, ""); err != nil {
			return fmt.Errorf("bind nsswitch.conf: %w", err)
		}
	case "varlink":
		if err := syscall.Mount("tmpfs", "/run/systemd/resolve", "tmpfs", 0, "mode=755"); err != nil {
			return fmt.Errorf("tmpfs over resolve dir: %w", err)
		}
		if err := fakeVarlink("/run/systemd/resolve/io.systemd.Resolve", h); err != nil {
			return err
		}
	}
	h.printf("setup: lo up, listeners on %s:%s dns=%q", host, port, c.dns)
	return nil
}
