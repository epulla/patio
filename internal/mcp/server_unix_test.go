//go:build unix

package mcp

import "syscall"

func init() { mkfifo = syscall.Mkfifo }
