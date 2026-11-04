package process

import "golang.org/x/sys/unix"

func darwinChildState(state int8) childState {
	// <sys/proc.h>: SSTOP=4, SZOMB=5. The unreaped direct child retains its PID.
	switch state {
	case 4:
		return childStopped
	case 5:
		return childExited
	default:
		return childRunning
	}
}

func ownedChildState(pid int) (childState, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return childRunning, err
	}
	return darwinChildState(info.Proc.P_stat), nil
}

func ownedGroupAlive(group int) (bool, error) {
	items, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", group)
	if err != nil {
		return false, err
	}
	for _, info := range items {
		if darwinChildState(info.Proc.P_stat) != childExited {
			return true, nil
		}
	}
	return false, nil
}
