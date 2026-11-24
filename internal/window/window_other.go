//go:build !darwin

package window

import "errors"

// Run is only implemented on macOS.
func Run(home, addr, version string) error {
	return errors.New("the layer8 window needs macOS; run `layer8 daemon` and open the UI in a browser")
}
