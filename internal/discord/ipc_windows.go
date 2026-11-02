//go:build windows

package discord

import (
	"context"
	"fmt"
	"net"

	"github.com/Microsoft/go-winio"
)

func dialDiscordIPC(ctx context.Context) (net.Conn, error) {
	for index := 0; index < 10; index++ {
		path := fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, index)
		conn, err := winio.DialPipeContext(ctx, path)
		if err == nil {
			return conn, nil
		}
	}
	return nil, fmt.Errorf("Discord IPC pipe not found")
}
