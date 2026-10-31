package ports

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func runProcessIdentity(pid int) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return "", fmt.Errorf("invalid process identity")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) <= 19 || fields[0] == "Z" || fields[0] == "X" {
		return "", fmt.Errorf("missing process identity")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(boot)) + ":" + fields[19], nil
}
