//go:build windows

package storage

import "os"

func posixModesEnforced() bool { return false }

func ownedByCurrentUser(os.FileInfo) bool { return true }
