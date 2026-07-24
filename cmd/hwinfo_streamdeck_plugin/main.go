package main

import (
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"

	plugin "github.com/moeilijk/hwinfo-streamdeck/internal/app/hwinfostreamdeckplugin"
)

var port = flag.String("port", "", "The port that should be used to create the WebSocket")
var pluginUUID = flag.String("pluginUUID", "", "A unique identifier string that should be used to register the plugin once the WebSocket is opened")
var registerEvent = flag.String("registerEvent", "", "Registration event")
var info = flag.String("info", "", "A stringified json containing the Stream Deck application information and devices information")

func main() {
	// make sure files are read relative to exe
	err := os.Chdir(filepath.Dir(os.Args[0]))
	if err != nil {
		log.Fatalf("Unable to chdir: %v", err)
	}

	// Disable logging in production
	logPath := filepath.Join(filepath.Dir(os.Args[0]), "hwinfo.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err == nil {
		log.SetOutput(f)
		log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	} else {
		log.SetOutput(io.Discard)
	}

	flag.Parse()

	p, err := plugin.NewPlugin(*port, *pluginUUID, *registerEvent, *info)
	if err != nil {
		log.Fatal("NewPlugin failed:", err)
	}

	err = p.RunForever()
	if err != nil {
		log.Fatal("runForever", err)
	}
}
