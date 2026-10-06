package cmd

import "net"

// splitHostPortLenient splits addr into host/port, tolerating a bare host
// with no port (net.SplitHostPort errors on that) by treating the whole
// string as the host in that case.
func splitHostPortLenient(addr string) (host, port string, err error) {
	host, port, err = net.SplitHostPort(addr)
	if err == nil {
		return host, port, nil
	}
	// No port present, or some other split failure: validate addr looks like
	// a bare host/IP by round-tripping it through net.JoinHostPort with a
	// throwaway port; if that still fails to split, it's not a valid host.
	if _, _, err2 := net.SplitHostPort(net.JoinHostPort(addr, "0")); err2 == nil {
		return addr, "", nil
	}
	return "", "", err
}
