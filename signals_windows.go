//go:build windows

package main

import "os"

// additionalSignals は Windows では追加シグナルなし。
func additionalSignals() []os.Signal {
	return nil
}
