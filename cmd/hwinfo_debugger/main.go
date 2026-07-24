package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"
)

var port = flag.String("port", "", "The port that should be used to create the WebSocket")
var pluginUUID = flag.String("pluginUUID", "", "A unique identifier string that should be used to register the plugin once the WebSocket is opened")
var registerEvent = flag.String("registerEvent", "", "Registration event")
var info = flag.String("info", "", "A stringified json containing the Stream Deck application information and devices information")

func main() {
	appdata := os.Getenv("APPDATA")
	logpath := filepath.Join(appdata, "Elgato/StreamDeck/Plugins/com.exension.hwinfo.sdPlugin/hwinfo.log")
	f, err := os.OpenFile(logpath, os.O_RDWR|os.O_CREATE, 0666)
	f.Truncate(0)
	if err != nil {
		log.Fatalf("OpenFile Log: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)
	log.SetFlags(0)

	flag.Parse()

	args := []string{
		"-port",
		*port,
		"-pluginUUID",
		*pluginUUID,
		"-registerEvent",
		*registerEvent,
		"-info",
		*info,
	}
	bytes, err := json.MarshalIndent(args, "", "    ")
	if err != nil {
		log.Fatal("Failed to marshal args", err)
	}

	log.Println(string(bytes))
}
