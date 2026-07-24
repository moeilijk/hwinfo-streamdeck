package streamdeck

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// debugMode enables verbose logging when HWINFO_DEBUG=1
var debugMode = os.Getenv("HWINFO_DEBUG") == "1"

func debugLog(format string, v ...interface{}) {
	if debugMode {
		log.Printf(format, v...)
	}
}

// EventDelegate receives callbacks for Stream Deck SDK events
type EventDelegate interface {
	OnConnected(*websocket.Conn)
	OnWillAppear(*EvWillAppear)
	OnWillDisappear(*EvWillDisappear)
	OnKeyDown(*EvKeyDown)
	OnDialDown(*EvDialDown)
	OnDialUp(*EvDialUp)
	OnDialRotate(*EvDialRotate)
	OnTouchTap(*EvTouchTap)
	OnDidReceiveSettings(*EvDidReceiveSettings)
	OnTitleParametersDidChange(*EvTitleParametersDidChange)
	OnPropertyInspectorConnected(*EvSendToPlugin)
	OnSendToPlugin(*EvSendToPlugin)
	OnApplicationDidLaunch(*EvApplication)
	OnApplicationDidTerminate(*EvApplication)
	OnDidReceiveGlobalSettings(*EvDidReceiveGlobalSettings)
}

// StreamDeck SDK APIs
type StreamDeck struct {
	Port          string
	PluginUUID    string
	RegisterEvent string
	Info          string
	delegate      EventDelegate
	conn          *websocket.Conn
	writeMu       sync.Mutex // serializes WebSocket writes (gorilla/websocket requires single concurrent writer)
	done          chan struct{}
}

// NewStreamDeck prepares StreamDeck struct
func NewStreamDeck(port, pluginUUID, registerEvent, info string) *StreamDeck {
	return &StreamDeck{
		Port:          port,
		PluginUUID:    pluginUUID,
		RegisterEvent: registerEvent,
		Info:          info,
		done:          make(chan struct{}),
	}
}

// SetDelegate sets the delegate for receiving Stream Deck SDK event callbacks
func (sd *StreamDeck) SetDelegate(ed EventDelegate) {
	sd.delegate = ed
}

func (sd *StreamDeck) register() error {
	reg := evRegister{Event: sd.RegisterEvent, UUID: sd.PluginUUID}
	data, err := json.Marshal(reg)
	debugLog("register: %s", data)
	if err != nil {
		return err
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return err
	}
	return nil
}

// Connect establishes WebSocket connection to StreamDeck software
func (sd *StreamDeck) Connect() error {
	u := url.URL{Scheme: "ws", Host: fmt.Sprintf("127.0.0.1:%s", sd.Port)}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}

	sd.conn = c

	err = sd.register()
	if err != nil {
		return fmt.Errorf("failed register: %v", err)
	}

	if sd.delegate != nil {
		sd.delegate.OnConnected(sd.conn)
	}

	return nil
}

// Close closes the websocket connection, defer after Connect
func (sd *StreamDeck) Close() {
	sd.conn.Close()
}

func (sd *StreamDeck) onPropertyInspectorMessage(value string, ev *EvSendToPlugin) error {
	switch value {
	case "propertyInspectorConnected":
		if sd.delegate != nil {
			sd.delegate.OnPropertyInspectorConnected(ev)
		}
	default:
		log.Printf("Unknown property_inspector value: %s\n", value)
	}
	return nil
}

func (sd *StreamDeck) onSendToPlugin(ev *EvSendToPlugin) error {
	payload := make(map[string]*json.RawMessage)
	err := json.Unmarshal(*ev.Payload, &payload)
	if err != nil {
		return fmt.Errorf("onSendToPlugin payload unmarshal: %v", err)
	}
	if raw, ok := payload["property_inspector"]; ok {
		var value string
		err := json.Unmarshal(*raw, &value)
		if err != nil {
			return fmt.Errorf("onSendToPlugin unmarshal property_inspector value: %v", err)
		}
		err = sd.onPropertyInspectorMessage(value, ev)
		if err != nil {
			return fmt.Errorf("onPropertyInspectorMessage: %v", err)
		}
		return nil
	}
	if sd.delegate != nil {
		sd.delegate.OnSendToPlugin(ev)
	}
	return nil
}

func (sd *StreamDeck) spawnMessageReader() {
	defer close(sd.done)
	for {
		_, message, err := sd.conn.ReadMessage()
		if err != nil {
			log.Println("read:", err)
			return
		}
		debugLog("recv: %s", message)

		var objmap map[string]*json.RawMessage
		err = json.Unmarshal(message, &objmap)
		if err != nil {
			log.Printf("message unmarshal: %v", err)
			continue
		}
		raw, ok := objmap["event"]
		if !ok || raw == nil {
			log.Printf("message missing event field")
			continue
		}
		var event string
		err = json.Unmarshal(*raw, &event)
		if err != nil {
			log.Printf("event unmarshal: %v", err)
			continue
		}
		switch event {
		case "willAppear":
			var ev EvWillAppear
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("willAppear unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnWillAppear(&ev)
			}
		case "willDisappear":
			var ev EvWillDisappear
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("willDisappear unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnWillDisappear(&ev)
			}
		case "keyDown":
			var ev EvKeyDown
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("keyDown unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnKeyDown(&ev)
			}
		case "dialDown":
			var ev EvDialDown
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("dialDown unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnDialDown(&ev)
			}
		case "dialUp":
			var ev EvDialUp
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("dialUp unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnDialUp(&ev)
			}
		case "dialRotate":
			var ev EvDialRotate
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("dialRotate unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnDialRotate(&ev)
			}
		case "touchTap":
			var ev EvTouchTap
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("touchTap unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnTouchTap(&ev)
			}
		case "didReceiveSettings":
			var ev EvDidReceiveSettings
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("didReceiveSettings unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnDidReceiveSettings(&ev)
			}
		case "didReceiveGlobalSettings":
			var ev EvDidReceiveGlobalSettings
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("didReceiveGlobalSettings unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnDidReceiveGlobalSettings(&ev)
			}
		case "titleParametersDidChange":
			var ev EvTitleParametersDidChange
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("titleParametersDidChange unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnTitleParametersDidChange(&ev)
			}
		case "sendToPlugin":
			var ev EvSendToPlugin
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("sendToPlugin unmarshal: %v", err)
				continue
			}
			if err := sd.onSendToPlugin(&ev); err != nil {
				log.Printf("onSendToPlugin: %v", err)
			}
		case "propertyInspectorDidAppear":
			var ev EvSendToPlugin
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("propertyInspectorDidAppear unmarshal: %v", err)
				continue
			}
			debugLog("propertyInspectorDidAppear dispatch")
			if sd.delegate != nil {
				sd.delegate.OnPropertyInspectorConnected(&ev)
			}
		case "applicationDidLaunch":
			var ev EvApplication
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("applicationDidLaunch unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnApplicationDidLaunch(&ev)
			}
		case "applicationDidTerminate":
			var ev EvApplication
			if err := json.Unmarshal(message, &ev); err != nil {
				log.Printf("applicationDidTerminate unmarshal: %v", err)
				continue
			}
			if sd.delegate != nil {
				sd.delegate.OnApplicationDidTerminate(&ev)
			}
		case "deviceDidConnect":
			// No-op: Stream Deck device connect event (not needed by this plugin).
		case "deviceDidDisconnect":
			// No-op: Stream Deck device disconnect event (not needed by this plugin).
		default:
			debugLog("Unknown event: %s\n", event)
		}
	}
}

// ListenAndWait processes messages and waits until closed
func (sd *StreamDeck) ListenAndWait() {
	go sd.spawnMessageReader()

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	for {
		select {
		case <-sd.done:
			return
		case <-interrupt:
			log.Println("interrupt")

			// Cleanly close the connection by sending a close message and then
			// waiting (with timeout) for the server to close the connection.
			sd.writeMu.Lock()
			err := sd.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			sd.writeMu.Unlock()
			if err != nil {
				log.Println("write close:", err)
				return
			}
			select {
			case <-sd.done:
			case <-time.After(time.Second):
			}
		}
	}
}

// SendToPropertyInspector sends a payload to the Property Inspector
func (sd *StreamDeck) SendToPropertyInspector(action, context string, payload interface{}) error {
	event := evSendToPropertyInspector{Action: action, Event: "sendToPropertyInspector",
		Context: context, Payload: payload}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("sendToPropertyInspector: %v", err)
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("sendToPropertyInspector write: %v", err)
	}
	return nil
}

// SetTitle dynamically changes the title displayed by an instance of an action
func (sd *StreamDeck) SetTitle(context, title string) error {
	event := evSetTitle{Event: "setTitle", Context: context, Payload: evSetTitlePayload{
		Title:  title,
		Target: 0,
	}}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("setTitle: %v", err)
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("setTitle write: %v", err)
	}
	return nil
}

// SetSettings saves persistent data for the action's instance
func (sd *StreamDeck) SetSettings(context string, payload interface{}) error {
	event := evSetSettings{Event: "setSettings", Context: context, Payload: payload}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("setSettings: %v", err)
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("setSettings write: %v", err)
	}
	return nil
}

// SetImage dynamically changes the image displayed by an instance of an action
func (sd *StreamDeck) SetImage(context string, bts []byte) error {
	b64 := base64.StdEncoding.EncodeToString(bts)
	event := evSetImage{Event: "setImage", Context: context, Payload: evSetImagePayload{
		Image:  fmt.Sprintf("data:image/png;base64, %s", b64),
		Target: 0,
	}}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("setImage: %v", err)
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("setImage write: %v", err)
	}
	return nil
}

// SetFeedback updates items in the current encoder feedback layout.
func (sd *StreamDeck) SetFeedback(context string, payload interface{}) error {
	event := evSetFeedback{Event: "setFeedback", Context: context, Payload: payload}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("setFeedback: %v", err)
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("setFeedback write: %v", err)
	}
	return nil
}

// SetFeedbackLayout assigns an encoder feedback layout, such as "$A0".
func (sd *StreamDeck) SetFeedbackLayout(context, layout string) error {
	event := evSetFeedbackLayout{Event: "setFeedbackLayout", Context: context, Payload: evSetFeedbackLayoutPayload{
		Layout: layout,
	}}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("setFeedbackLayout: %v", err)
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("setFeedbackLayout write: %v", err)
	}
	return nil
}

// SetTriggerDescription updates Stream Deck+ encoder trigger descriptions.
func (sd *StreamDeck) SetTriggerDescription(context string, payload interface{}) error {
	event := evSetTriggerDescription{Event: "setTriggerDescription", Context: context, Payload: payload}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("setTriggerDescription: %v", err)
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("setTriggerDescription write: %v", err)
	}
	return nil
}

// GetGlobalSettings requests the global settings from Stream Deck
func (sd *StreamDeck) GetGlobalSettings() error {
	event := struct {
		Event   string `json:"event"`
		Context string `json:"context"`
	}{
		Event:   "getGlobalSettings",
		Context: sd.PluginUUID,
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("getGlobalSettings: %v", err)
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("getGlobalSettings write: %v", err)
	}
	return nil
}

// SetGlobalSettings saves persistent global settings
func (sd *StreamDeck) SetGlobalSettings(payload interface{}) error {
	event := struct {
		Event   string      `json:"event"`
		Context string      `json:"context"`
		Payload interface{} `json:"payload"`
	}{
		Event:   "setGlobalSettings",
		Context: sd.PluginUUID,
		Payload: payload,
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("setGlobalSettings: %v", err)
	}
	sd.writeMu.Lock()
	err = sd.conn.WriteMessage(websocket.TextMessage, data)
	sd.writeMu.Unlock()
	if err != nil {
		return fmt.Errorf("setGlobalSettings write: %v", err)
	}
	return nil
}
