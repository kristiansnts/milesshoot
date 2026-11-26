//go:build !darwin && !windows

package update

import "errors"

var errUnsupported = errors.New("pembaruan otomatis belum didukung di sistem ini")

func CurrentBundle() (string, error)                { return "", errUnsupported }
func CanReplace() error                             { return errUnsupported }
func Apply(string, string, int, string, bool) error { return errUnsupported }
