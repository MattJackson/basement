// Command basement-server starts the admin + user server.
package main

import (
	"fmt"
	"os"

	driverpkg "github.com/mattjackson/basement/internal/driver"
	_ "github.com/mattjackson/basement/internal/drivers/garage"
)

func main() {
	fmt.Fprintf(os.Stderr, "registered drivers: %v\n", driverpkg.Registered())
	fmt.Fprintln(os.Stderr, "basement (not implemented)")
	os.Exit(1)
}
