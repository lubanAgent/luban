// This executable verifies package registration after a trimpath build without
// importing the company's startup adapters or opening external connections.
package main

import (
	"log"
	"os"

	_ "github.com/lubanAgent/luban/agents/callsummary"
	"github.com/lubanAgent/luban/server"
)

func main() {
	app, err := server.New(server.Options{Assets: os.DirFS(os.Args[1])})
	if err != nil {
		log.Fatal(err)
	}
	_, code := app.RunCLI(os.Args[2:])
	_ = app.Close()
	os.Exit(code)
}
