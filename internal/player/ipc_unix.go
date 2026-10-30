//go:build !windows

package player

import (
	"context"
	"net"
	"os"
	"path/filepath"
)

func ipcEndpoint(directory string) (string, func(), error) {
	path := filepath.Join(directory, "ipc")
	cleanup := func() {}
	// macOS's default temporary directory can exceed sockaddr_un.sun_path.
	if len(path) >= 100 {
		short, err := os.MkdirTemp("/tmp", "animix-ipc-")
		if err != nil {
			return "", cleanup, err
		}
		path = filepath.Join(short, "ipc")
		cleanup = func() { _ = os.RemoveAll(short) }
	}
	return path, cleanup, nil
}

func dialIPC(ctx context.Context, endpoint string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", endpoint)
}
