// Command schemagen regenerates the committed schema documents from the struct
// definitions they describe. Everything it does lives in internal/schemadoc;
// this package holds nothing but func main, which no test can call, so that
// nothing untestable sits next to code that is.
package main

import (
	"fmt"
	"os"

	"github.com/pedromvgomes/agentic-toolkit/internal/schemadoc"
)

func main() {
	if err := schemadoc.Generate(); err != nil {
		fmt.Fprintln(os.Stderr, "schemagen:", err)
		os.Exit(1)
	}
}
