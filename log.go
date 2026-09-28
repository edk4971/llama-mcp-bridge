//go:build !nostderr
// +build !nostderr

package main

import (
	"fmt"
	"os"
)

func logStderr(msg string) {
	fmt.Fprintln(os.Stderr, msg)
}
