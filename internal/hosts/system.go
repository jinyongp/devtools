// Package hosts manages a bounded devtools-owned hosts block. Requests cannot
// select an arbitrary privileged write target.
package hosts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/protocol"
)

const begin = "# BEGIN devtools managed hosts"
const end = "# END devtools managed hosts"

type Mapping struct {
	Host    string `json:"host"`
	Address string `json:"address"`
}
type Request struct {
	Expected string    `json:"expected_digest"`
	Mappings []Mapping `json:"mappings"`
}
type System struct{ Path string }

func failure(code string) *protocol.Error {
	return protocol.NewError(code, "Hosts preparation could not safely apply the selected mappings.", 3, nil)
}
func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func (s System) path() string {
	if s.Path != "" {
		return s.Path
	}
	return "/etc/hosts"
}
func (s System) Read() ([]byte, *protocol.Error) {
	f, e := os.OpenFile(s.path(), os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, failure("hosts_unavailable")
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() {
		return nil, failure("hosts_unavailable")
	}
	b, e := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if e != nil || len(b) > 1<<20 {
		return nil, failure("hosts_unavailable")
	}
	return b, nil
}
func split(b []byte) ([]byte, []byte, *protocol.Error) {
	start, finish := bytes.Index(b, []byte(begin)), bytes.Index(b, []byte(end))
	if start < 0 && finish < 0 {
		return b, nil, nil
	}
	if start < 0 || finish < start || bytes.Count(b, []byte(begin)) != 1 || bytes.Count(b, []byte(end)) != 1 || start > 0 && b[start-1] != '\n' {
		return nil, nil, failure("hosts_conflict")
	}
	last := finish + len(end)
	if last < len(b) && b[last] != '\n' && b[last] != '\r' {
		return nil, nil, failure("hosts_conflict")
	}
	if last < len(b) && b[last] == '\r' {
		last++
	}
	if last < len(b) && b[last] == '\n' {
		last++
	}
	outside := append(append([]byte{}, b[:start]...), b[last:]...)
	return outside, b[start:last], nil
}

// A marked block can belong to another user data root or an interrupted older
// installation. Its marker alone does not authorize replacing unknown mappings.
func validateOwnership(before []byte, known []Mapping) *protocol.Error {
	_, block, e := split(before)
	if e != nil {
		return e
	}
	owned := map[Mapping]bool{}
	for _, m := range known {
		owned[m] = true
	}
	for _, line := range strings.Split(string(block), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == begin || line == end {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !owned[Mapping{Host: fields[1], Address: fields[0]}] {
			return failure("hosts_ownership_unknown")
		}
	}
	return nil
}

// Render preserves external bytes and never takes ownership of an existing
// external mapping. Conflicts fail before writing or requesting permission.
func Render(before []byte, mappings []Mapping) ([]byte, *protocol.Error) {
	outside, _, e := split(before)
	if e != nil {
		return nil, e
	}
	wanted := map[string]string{}
	for _, m := range mappings {
		ip, e := netip.ParseAddr(m.Address)
		if e != nil || !ip.IsLoopback() || !network.ValidHost(m.Host) {
			return nil, failure("invalid_argument")
		}
		if old, ok := wanted[m.Host]; ok && old != m.Address {
			return nil, failure("hosts_conflict")
		}
		wanted[m.Host] = m.Address
	}
	external := map[string]bool{}
	for _, line := range strings.Split(string(outside), "\n") {
		text, _, _ := strings.Cut(line, "#")
		fields := strings.Fields(text)
		if len(fields) < 2 {
			continue
		}
		for _, host := range fields[1:] {
			if address, ok := wanted[strings.ToLower(host)]; ok {
				ip, err := netip.ParseAddr(fields[0])
				target, _ := netip.ParseAddr(address)
				if err != nil || ip.Unmap() != target.Unmap() {
					return nil, failure("hosts_conflict")
				}
				external[strings.ToLower(host)] = true
			}
		}
	}
	for host := range external {
		delete(wanted, host)
	}
	if len(wanted) == 0 {
		return outside, nil
	}
	names := make([]string, 0, len(wanted))
	for host := range wanted {
		names = append(names, host)
	}
	sort.Strings(names)
	var block strings.Builder
	block.WriteString(begin + "\n")
	for _, host := range names {
		block.WriteString(wanted[host] + "\t" + host + "\n")
	}
	block.WriteString(end + "\n")
	if len(outside) > 0 && outside[len(outside)-1] != '\n' {
		outside = append(outside, '\n')
	}
	return append(outside, []byte(block.String())...), nil
}
func (s System) Apply(ctx context.Context, q Request) *protocol.Error {
	if ctx.Err() != nil {
		return failure("canceled")
	}
	before, e := s.Read()
	if e != nil {
		return e
	}
	if digest(before) != q.Expected {
		return failure("stale_preview")
	}
	after, e := Render(before, q.Mappings)
	if e != nil {
		return e
	}
	if bytes.Equal(before, after) {
		return nil
	}
	info, err := os.Lstat(s.path())
	if err != nil || !info.Mode().IsRegular() {
		return failure("hosts_unavailable")
	}
	f, err := os.CreateTemp(filepath.Dir(s.path()), ".devtools-hosts-*")
	if err != nil {
		return failure("permission_required")
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()
	if err = f.Chmod(info.Mode().Perm()); err != nil {
		return failure("hosts_unavailable")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		if err = f.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
			return failure("hosts_unavailable")
		}
	}
	if _, err = f.Write(after); err != nil {
		return failure("hosts_unavailable")
	}
	if f.Sync() != nil || f.Close() != nil {
		return failure("hosts_unavailable")
	}
	current, e := s.Read()
	if e != nil {
		return e
	}
	currentInfo, err := os.Lstat(s.path())
	if err != nil || !os.SameFile(info, currentInfo) || !bytes.Equal(current, before) {
		return failure("stale_preview")
	}
	if err = os.Rename(name, s.path()); err == nil {
		return nil
	}
	if !errors.Is(err, syscall.EBUSY) {
		return failure("hosts_unavailable")
	}
	// WSL and containers can mount /etc/hosts as a single file. Keep its
	// inode and metadata, lock the opened file, and recheck the frozen digest.
	file, err := os.OpenFile(s.path(), os.O_RDWR|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return failure("permission_required")
	}
	defer file.Close()
	if syscall.Flock(int(file.Fd()), syscall.LOCK_EX) != nil {
		return failure("hosts_unavailable")
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	current, err = io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil || !bytes.Equal(current, before) {
		return failure("stale_preview")
	}
	if _, err = file.WriteAt(after, 0); err != nil {
		_, _ = file.WriteAt(before, 0)
		_ = file.Truncate(int64(len(before)))
		return failure("recovery_required")
	}
	if file.Truncate(int64(len(after))) != nil || file.Sync() != nil {
		return failure("recovery_required")
	}
	return nil
}
