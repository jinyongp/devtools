package services

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jinyongp/devtools/internal/network"
	"github.com/jinyongp/devtools/internal/platformfs"
	"github.com/jinyongp/devtools/internal/tasks"
	"golang.org/x/sys/windows"
)

func windowsStartingRecord(t *testing.T) (Store, Record) {
	t.Helper()
	store := Store{Data: filepath.Join(t.TempDir(), "data")}
	record := Record{ID: tasks.ID(), Profile: "test", Instance: tasks.ID(), Command: "fixture", Directory: t.TempDir(), State: "starting", CreatedAt: time.Now().UTC()}
	return store, record
}
func TestWindowsLaunchUsesStableExternalLeaseAndV2Marker(t *testing.T) {
	store, record := windowsStartingRecord(t)
	lease, e := store.prepareLaunch(record)
	if e != nil {
		t.Fatal(e)
	}
	defer lease.unlockClose()
	if !strings.Contains(store.leasePath(record.ID), string(filepath.Separator)+"process-leases"+string(filepath.Separator)) {
		t.Fatal("lease remained inside movable execution directory")
	}
	if _, err := os.Stat(store.path(record.ID, "lease.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("execution contains a movable Windows lease")
	}
	marker, found, e := store.readLaunchMarker(record.ID)
	if e != nil || !found || marker.Version != 2 || marker.LeaseHandoff != "inherited-handle" {
		t.Fatalf("native marker: %+v %v %v", marker, found, e)
	}
	available, e := store.leaseAvailable(context.Background(), record.ID, false)
	if e != nil || available {
		t.Fatalf("held lease: %v %v", available, e)
	}
	if err := lease.closeReference(); err != nil {
		t.Fatal(err)
	}
	available, e = store.leaseAvailable(context.Background(), record.ID, false)
	if e != nil || !available {
		t.Fatalf("closed lease: %v %v", available, e)
	}
	if e = store.cleanupLaunchStaging(); e != nil {
		t.Fatal(e)
	}
	if _, err := os.Stat(store.leasePath(record.ID)); err != nil {
		t.Fatal("cleanup removed retained fixed lease")
	}
	root, err := platformfs.OpenRoot(store.path(record.ID, ""), false)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	data, err := root.ReadFile("record.json", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	var got Record
	if err = json.Unmarshal(data, &got); err != nil || got.ID != record.ID {
		t.Fatal("record publication lost identity")
	}
}
func TestWindowsLaunchRejectsDuplicateAndInvalidSnapshot(t *testing.T) {
	store, record := windowsStartingRecord(t)
	lease, e := store.prepareLaunch(record)
	if e != nil {
		t.Fatal(e)
	}
	defer lease.unlockClose()
	if other, e := store.prepareLaunch(record); e == nil {
		other.unlockClose()
		t.Fatal("duplicate launch bypassed held lease")
	}
	if err := lease.closeReference(); err != nil {
		t.Fatal(err)
	}
	if other, e := store.prepareLaunch(record); e == nil {
		other.unlockClose()
		t.Fatal("existing published execution overwritten")
	}
	other := record
	other.ID = tasks.ID()
	other.ProxyMode = true
	other.NetworkSnapshotID = tasks.ID()
	if candidate, e := store.prepareLaunch(other, &network.ExecutionSnapshot{ID: tasks.ID()}); e == nil || e.Code != "network_snapshot_required" {
		if candidate != nil {
			candidate.unlockClose()
		}
		t.Fatalf("invalid snapshot: %v", e)
	}
	entries, err := os.ReadDir(store.root())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".launch-") {
			t.Fatal("failed launch leaked stage")
		}
	}
	if _, err := os.Stat(store.leasePath(other.ID)); err != nil {
		t.Fatal("failed launch's empty lease should be retained")
	}
}
func TestWindowsCleanupOnlyRemovesPrivateFixedPatternStages(t *testing.T) {
	store, record := windowsStartingRecord(t)
	root, err := platformfs.OpenRoot(store.Data, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	stage := launchStagingName(record.ID) + "a123"
	if !validLaunchStagingName(stage) {
		t.Fatal("fixture is not a valid stage")
	}
	for _, name := range []string{filepath.Join("processes", stage), filepath.Join("processes", ".launch-unrelated"), "process-leases"} {
		if err = root.MkdirAll(name); err != nil {
			t.Fatal(err)
		}
	}
	lease, acquired, err := tryLease(store.leasePath(record.ID), true)
	if err != nil || !acquired {
		t.Fatal(err)
	}
	defer lease.closeReference()
	if e := store.cleanupLaunchStaging(); e != nil {
		t.Fatal(e)
	}
	if _, err = os.Stat(filepath.Join(store.root(), stage)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("orphan stage remained")
	}
	if _, err = os.Stat(filepath.Join(store.root(), ".launch-unrelated")); err != nil {
		t.Fatal("cleanup removed unrelated directory")
	}
	if _, err = os.Stat(store.leasePath(record.ID)); err != nil {
		t.Fatal("cleanup removed fixed lease")
	}
}
func TestWindowsLaunchMarkerRejectsUnixHandoff(t *testing.T) {
	if validLaunchMarker(launchMarker{Version: 1, LeaseHandoff: "inherited-fd"}) {
		t.Fatal("Unix lease format accepted as Windows ownership")
	}
	if !validLaunchMarker(platformLaunchMarker()) {
		t.Fatal("native lease format rejected")
	}
}

func TestWindowsAdoptRejectsSharedAndMetadataOnlyHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private")
	root, err := platformfs.OpenRoot(path, true)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	file, err := root.OpenFile("lease", os.O_CREATE|os.O_RDWR)
	if err != nil {
		t.Fatal(err)
	}
	if adopted, err := adoptInheritedLease(file.File, file.Name()); err == nil {
		adopted.closeReference()
		t.Fatal("shared handle adopted as exclusive lease")
	}
	if _, err = file.Stat(); err != nil {
		t.Fatal("failed adoption closed its input")
	}
	name := file.Name()
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	ptr, _ := windows.UTF16PtrFromString(name)
	handle, err := windows.CreateFile(ptr, windows.READ_CONTROL|windows.FILE_READ_ATTRIBUTES, 0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	metadata := os.NewFile(uintptr(handle), name)
	defer metadata.Close()
	if adopted, err := adoptInheritedLease(metadata, name); err == nil {
		adopted.closeReference()
		t.Fatal("metadata-only handle adopted as lease")
	}
	if _, err = metadata.Stat(); err != nil {
		t.Fatal("metadata input closed on rejection")
	}
}

func TestWindowsServiceInheritedLeaseChild(t *testing.T) {
	if os.Getenv("DEVTOOLS_SERVICE_LEASE_CHILD") != "1" {
		return
	}
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) != 2 {
		os.Exit(90)
	}
	number, err := strconv.ParseUint(args[0], 10, 64)
	if err != nil {
		os.Exit(91)
	}
	file := os.NewFile(uintptr(number), "inherited-lease")
	lease, err := adoptInheritedLease(file, args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(92)
	}
	fmt.Fprintln(os.Stdout, "held")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if err = lease.closeReference(); err != nil {
		os.Exit(93)
	}
	os.Exit(0)
}
func TestWindowsServiceInheritedLeaseHasNoParentCloseGap(t *testing.T) {
	store, record := windowsStartingRecord(t)
	lease, e := store.prepareLaunch(record)
	if e != nil {
		t.Fatal(e)
	}
	defer lease.closeReference()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=TestWindowsServiceInheritedLeaseChild", "--", strconv.FormatUint(uint64(lease.file.Fd()), 10), store.leasePath(record.ID))
	cmd.Env = append(os.Environ(), "DEVTOOLS_SERVICE_LEASE_CHILD=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(lease.file.Fd())}}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var diagnostic strings.Builder
	cmd.Stderr = &diagnostic
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	line, err := bufio.NewReader(output).ReadString('\n')
	if err != nil || strings.TrimSpace(line) != "held" {
		t.Fatalf("service lease handoff: %q %v %s", line, err, &diagnostic)
	}
	if err = lease.closeReference(); err != nil {
		t.Fatal(err)
	}
	available, e := store.leaseAvailable(context.Background(), record.ID, false)
	if e != nil || available {
		t.Fatal("service inherited reference lost exclusion")
	}
	if err = input.Close(); err != nil {
		t.Fatal(err)
	}
	if err = cmd.Wait(); err != nil {
		t.Fatalf("service child: %v %s", err, &diagnostic)
	}
	available, e = store.leaseAvailable(context.Background(), record.ID, false)
	if e != nil || !available {
		t.Fatal("exited service child retained lease")
	}
}
