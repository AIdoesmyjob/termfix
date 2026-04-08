package main

import (
	"github.com/AIdoesmyjob/termfix/cmd"
	"github.com/AIdoesmyjob/termfix/internal/logging"
)

func main() {
	defer logging.RecoverPanic("main", func() {
		logging.ErrorPersist("Application terminated due to unhandled panic")
	})

	cmd.Execute()
}
