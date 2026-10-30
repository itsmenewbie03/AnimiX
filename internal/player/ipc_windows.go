package player

import (
	"context"
	"net"
	"path/filepath"

	"github.com/Microsoft/go-winio"
)

func ipcEndpoint(directory string) (string, func(), error) {
	return `\\.\pipe\` + filepath.Base(directory), func() {}, nil
}

func dialIPC(ctx context.Context, endpoint string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, endpoint)
}
