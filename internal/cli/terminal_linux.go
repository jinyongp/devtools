package cli

import "golang.org/x/sys/unix"

const terminalReadState = unix.TCGETS
const terminalWriteState = unix.TCSETS
const terminalFlushState = unix.TCSETSF
