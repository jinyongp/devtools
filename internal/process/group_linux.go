package process

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func readProcessState(pid int) (childState, int, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return childRunning, 0, err
	}
	// comm may contain spaces and parentheses. Fields after its final ')' are
	// state, ppid, pgrp, ...; never split the process name as ordinary fields.
	end := strings.LastIndexByte(string(data), ')')
	if end < 0 {
		return childRunning, 0, errors.New("invalid process state")
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 3 {
		return childRunning, 0, errors.New("invalid process state")
	}
	group, err := strconv.Atoi(fields[2])
	state := childRunning
	switch fields[0] {
	case "Z", "X":
		state = childExited
	case "T", "t":
		state = childStopped
	}
	return state, group, err
}

func ownedChildState(pid int) (childState, error) {
	state, _, err := readProcessState(pid)
	return state, err
}

func ownedGroupAlive(group int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		state, observed, err := readProcessState(pid)
		if err != nil {
			if actual, queryErr := syscall.Getpgid(pid); queryErr == nil && actual == group {
				return false, err
			}
			continue
		}
		if observed == group && state != childExited {
			return true, nil
		}
	}
	return false, nil
}
