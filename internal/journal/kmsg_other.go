//go:build !linux

package journal

import "errors"

func readKmsg(string) ([]kmsgRecord, error) { return nil, errors.New("not linux") }
