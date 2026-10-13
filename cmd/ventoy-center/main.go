package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/SkyWalkerAMD-org/ventoy/internal/center"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: ventoy-center init --dir DIR | serve --config FILE")
		os.Exit(2)
	}
	f := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	switch os.Args[1] {
	case "version":
		fmt.Println(center.Version)
	case "init":
		dir := f.String("dir", "runtime", "private configuration directory")
		f.Parse(os.Args[2:])
		if err := center.Initialize(*dir); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Created configuration and admin.token in", *dir)
	case "serve":
		path := f.String("config", "runtime/config.json", "configuration file")
		f.Parse(os.Args[2:])
		if err := center.Serve(*path); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatal("unknown command")
	}
}
