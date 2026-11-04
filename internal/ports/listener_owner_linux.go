package ports

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func listenerProcesses(ctx context.Context, port int) ([]ListenerProcess, error) {
	inodes := map[string]bool{}
	for _, table := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		file, err := os.Open(table)
		if errors.Is(err, os.ErrNotExist) && strings.HasSuffix(table, "tcp6") {
			continue
		}
		if err != nil {
			return nil, err
		}
		bounded := &io.LimitedReader{R: file, N: 8<<20 + 1}
		scanner := bufio.NewScanner(bounded)
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				file.Close()
				return nil, err
			}
			fields := strings.Fields(scanner.Text())
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			_, local, ok := strings.Cut(fields[1], ":")
			number, err := strconv.ParseUint(local, 16, 16)
			if ok && err == nil && int(number) == port {
				inodes["socket:["+fields[9]+"]"] = true
			}
		}
		err = scanner.Err()
		file.Close()
		if err != nil {
			return nil, err
		}
		if bounded.N == 0 {
			return nil, io.ErrShortBuffer
		}
	}
	if len(inodes) == 0 {
		return nil, nil
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var result []ListenerProcess
	var denied error
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 0 {
			continue
		}
		identity, err := runProcessIdentity(pid)
		if err != nil {
			continue
		}
		fdRoot := filepath.Join("/proc", entry.Name(), "fd")
		directory, err := os.Open(fdRoot)
		if err != nil {
			if errors.Is(err, os.ErrPermission) {
				denied = err
			}
			continue
		}
		found := false
		for !found && ctx.Err() == nil {
			files, readErr := directory.ReadDir(128)
			for _, fd := range files {
				target, err := os.Readlink(filepath.Join(fdRoot, fd.Name()))
				if err == nil && inodes[target] {
					found = true
					break
				}
			}
			if readErr != nil {
				break
			}
		}
		directory.Close()
		if found {
			again, err := runProcessIdentity(pid)
			if err == nil && identity == again {
				name, _ := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
				result = append(result, ListenerProcess{PID: pid, Program: strings.TrimSpace(string(name))})
			}
		}
	}
	if len(result) == 0 {
		return result, denied
	}
	return result, ctx.Err()
}
