package cliui

import (
	"strconv"
	"strings"
)

type Shell uint8

const (
	POSIX Shell = iota + 1
	Fish
	PowerShell
)

func safeWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_./:-", r)) {
			return false
		}
	}
	return true
}

func commandLine(argv []string, shell Shell) (string, bool) {
	if shell < POSIX || shell > PowerShell {
		return "", false
	}
	parts := make([]string, len(argv))
	for i, arg := range argv {
		for _, r := range arg {
			if escapedControl(r) || r == '\n' {
				return "", false
			}
		}
		if shell == PowerShell && (arg == "" || strings.ContainsAny(arg, "\"‘’“”") || strings.HasSuffix(arg, "\\") && strings.ContainsAny(arg, " \t")) {
			// Windows PowerShell's legacy native argument passing cannot preserve
			// every literal accepted by modern PowerShell. Never advertise a lossy line.
			return "", false
		}
		if safeWord(arg) {
			parts[i] = arg
			continue
		}
		switch shell {
		case POSIX:
			parts[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
		case Fish:
			parts[i] = "'" + strings.ReplaceAll(strings.ReplaceAll(arg, "\\", "\\\\"), "'", "\\'") + "'"
		case PowerShell:
			parts[i] = "'" + strings.ReplaceAll(arg, "'", "''") + "'"
		}
	}
	line := strings.Join(parts, " ")
	if shell == PowerShell && !safeWord(argv[0]) {
		line = "& " + line
	}
	return line, true
}

func argumentDisplay(argv []string) string {
	quoted := make([]string, len(argv))
	for i, arg := range argv {
		quoted[i] = strconv.Quote(arg)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

func shellLabel(shell Shell) string {
	switch shell {
	case POSIX:
		return "sh/bash/zsh"
	case Fish:
		return "fish"
	case PowerShell:
		return "PowerShell"
	}
	return "unsupported"
}
