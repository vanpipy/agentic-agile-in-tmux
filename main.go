// awp — A pi-native task collaboration board.
//
// Entry point: delegates to cmd/awp.Execute() which routes Cobra commands.
// See SYSTEM_DESIGN.md for the full architecture.
package main

import (
	"fmt"
	"os"

	"github.com/pi/awp/cmd/awp"
)

func main() {
	if err := awp.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "awp:", err)
		os.Exit(1)
	}
}
