//go:build !windows

package storage

import (
	"os"
	"syscall"
)

func posixModesEnforced() bool { return true }

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return true
	}
	return int(stat.Uid) == os.Geteuid()
}
