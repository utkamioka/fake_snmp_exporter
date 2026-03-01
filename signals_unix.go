//go:build !windows

package main

import (
	"os"
	"syscall"
)

// additionalSignals は Unix 系で追加するシグナルを返します。
func additionalSignals() []os.Signal {
	return []os.Signal{syscall.SIGTERM}
}
