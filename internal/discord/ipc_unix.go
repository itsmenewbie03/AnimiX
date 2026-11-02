//go:build !windows

package discord

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
)

func dialDiscordIPC(ctx context.Context) (net.Conn, error) {
	directories := []string{os.Getenv("XDG_RUNTIME_DIR"), os.Getenv("TMPDIR"), os.Getenv("TMP"), os.Getenv("TEMP"), "/tmp"}
	if uid := os.Getuid(); uid >= 0 {
		directories = append(directories, filepath.Join("/run/user", strconv.Itoa(uid)))
	}
	seen := make(map[string]bool)
	dialer := net.Dialer{}
	for _, directory := range directories {
		if directory == "" || seen[directory] {
			continue
		}
		seen[directory] = true
		for index := 0; index < 10; index++ {
			name := fmt.Sprintf("discord-ipc-%d", index)
			for _, path := range []string{
				filepath.Join(directory, name),
				filepath.Join(directory, "app", "com.discordapp.Discord", name),
				filepath.Join(directory, "snap.discord", name),
			} {
				conn, err := dialer.DialContext(ctx, "unix", path)
				if err == nil {
					return conn, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("Discord IPC socket not found")
}
