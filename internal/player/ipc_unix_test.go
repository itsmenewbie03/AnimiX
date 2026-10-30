//go:build !windows

package player

import "net"

func listenIPC(endpoint string) (net.Listener, error) { return net.Listen("unix", endpoint) }
