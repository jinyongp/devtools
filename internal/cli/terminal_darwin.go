package cli

import "golang.org/x/sys/unix"

const terminalReadState = unix.TIOCGETA
const terminalWriteState = unix.TIOCSETA
