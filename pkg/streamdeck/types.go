package streamdeck

import "encoding/json"

type evRegister struct {
	Event string `json:"event"`
	UUID  string `json:"uuid"`
}

// EvCoordinates is the coordinates structure from events
type EvCoordinates struct {
	Column int `json:"column"`
	Row    int `json:"row"`
}

// EvWillAppearPayload is the Payload structure from the willAppear event
type EvWillAppearPayload struct {
	Settings        *json.RawMessage `json:"settings"`
	Controller      string           `json:"controller"`
	Coordinates     EvCoordinates    `json:"coordinates"`
	Device          string           `json:"device"`
	State           int              `json:"state"`
	IsInMultiAction bool             `json:"isInMultiAction"`
}

// EvWillAppear is the payload from the willAppear event
type EvWillAppear struct {
	Action  string              `json:"action"`
	Event   string              `json:"event"`
	Context string              `json:"context"`
	Device  string              `json:"device"`
	Payload EvWillAppearPayload `json:"payload"`
}

// EvWillDisappearPayload is the Payload structure from willDisappear event
type EvWillDisappearPayload struct {
	EvWillAppearPayload
}

// EvWillDisappear is the payload from the willDisappear event
type EvWillDisappear struct {
	EvWillAppear
}

// EvKeyDownPayload is the payload structure from the keyDown event.
type EvKeyDownPayload struct {
	Settings         *json.RawMessage `json:"settings"`
	Controller       string           `json:"controller"`
	Coordinates      EvCoordinates    `json:"coordinates"`
	State            int              `json:"state"`
	UserDesiredState int              `json:"userDesiredState"`
	IsInMultiAction  bool             `json:"isInMultiAction"`
}

// EvKeyDown is the payload from the keyDown event.
type EvKeyDown struct {
	Action  string           `json:"action"`
	Event   string           `json:"event"`
	Context string           `json:"context"`
	Device  string           `json:"device"`
	Payload EvKeyDownPayload `json:"payload"`
}

// EvDialPayload is the common payload for Stream Deck+ dial press/release events.
type EvDialPayload struct {
	Settings    *json.RawMessage `json:"settings"`
	Controller  string           `json:"controller"`
	Coordinates EvCoordinates    `json:"coordinates"`
}

// EvDialDown is the payload from the dialDown event.
type EvDialDown struct {
	Action  string        `json:"action"`
	Event   string        `json:"event"`
	Context string        `json:"context"`
	Device  string        `json:"device"`
	Payload EvDialPayload `json:"payload"`
}

// EvDialUp is the payload from the dialUp event.
type EvDialUp struct {
	Action  string        `json:"action"`
	Event   string        `json:"event"`
	Context string        `json:"context"`
	Device  string        `json:"device"`
	Payload EvDialPayload `json:"payload"`
}

// EvDialRotatePayload is the payload from the dialRotate event.
type EvDialRotatePayload struct {
	Settings    *json.RawMessage `json:"settings"`
	Controller  string           `json:"controller"`
	Coordinates EvCoordinates    `json:"coordinates"`
	Pressed     bool             `json:"pressed"`
	Ticks       int              `json:"ticks"`
}

// EvDialRotate is the payload from the dialRotate event.
type EvDialRotate struct {
	Action  string              `json:"action"`
	Event   string              `json:"event"`
	Context string              `json:"context"`
	Device  string              `json:"device"`
	Payload EvDialRotatePayload `json:"payload"`
}

// EvTouchTapPayload is the payload from the touchTap event.
type EvTouchTapPayload struct {
	Settings    *json.RawMessage `json:"settings"`
	Controller  string           `json:"controller"`
	Coordinates EvCoordinates    `json:"coordinates"`
	Hold        bool             `json:"hold"`
	TapPos      []int            `json:"tapPos"`
}

// EvTouchTap is the payload from the touchTap event.
type EvTouchTap struct {
	Action  string            `json:"action"`
	Event   string            `json:"event"`
	Context string            `json:"context"`
	Device  string            `json:"device"`
	Payload EvTouchTapPayload `json:"payload"`
}

// EvApplicationPayload is the sub-strcture from the EvApplication struct
type EvApplicationPayload struct {
	Application string `json:"application"`
}

// EvApplication is the payload from the applicatioDidLaunch/Terminate events
type EvApplication struct {
	Payload EvApplicationPayload `json:"payload"`
}

// EvTitleParameters is sub-structure from EvTitleParametersDidChangePayload
type EvTitleParameters struct {
	FontFamily     string `json:"fontFamily"`
	FontSize       int    `json:"fontSize"`
	FontStyle      string `json:"fontStyle"`
	FontUnderline  bool   `json:"fontUnderline"`
	ShowTitle      bool   `json:"showTitle"`
	TitleAlignment string `json:"titleAlignment"`
	TitleColor     string `json:"titleColor"`
}

// EvTitleParametersDidChangePayload is the payload structure of EvTitleParametersDidChange
type EvTitleParametersDidChangePayload struct {
	Coordinates     EvCoordinates     `json:"coordinates"`
	Settings        *json.RawMessage  `json:"settings"`
	Controller      string            `json:"controller"`
	TitleParameters EvTitleParameters `json:"titleParameters"`
	Title           string            `json:"title"`
	State           int               `json:"state"`
}

// EvTitleParametersDidChange is the payload from the titleParametersDidChange event
type EvTitleParametersDidChange struct {
	Action  string                            `json:"action"`
	Event   string                            `json:"event"`
	Context string                            `json:"context"`
	Device  string                            `json:"device"`
	Payload EvTitleParametersDidChangePayload `json:"payload"`
}

// EvSendToPlugin is received from the Property Inspector
type EvSendToPlugin struct {
	Action  string           `json:"action"`
	Event   string           `json:"event"`
	Context string           `json:"context"`
	Payload *json.RawMessage `json:"payload"`
}

// EvDidReceiveSettingsPayload is the payload from didReceiveSettings event.
type EvDidReceiveSettingsPayload struct {
	Settings *json.RawMessage `json:"settings"`
}

// EvDidReceiveSettings is emitted after setSettings for an action instance.
type EvDidReceiveSettings struct {
	Action  string                      `json:"action"`
	Event   string                      `json:"event"`
	Context string                      `json:"context"`
	Device  string                      `json:"device"`
	Payload EvDidReceiveSettingsPayload `json:"payload"`
}

type evSendToPropertyInspector struct {
	Action  string      `json:"action"`
	Event   string      `json:"event"`
	Context string      `json:"context"`
	Payload interface{} `json:"payload"`
}

type evSetTitlePayload struct {
	Title  string `json:"title"`
	Target int    `json:"target"`
}

type evSetTitle struct {
	Event   string            `json:"event"`
	Context string            `json:"context"`
	Payload evSetTitlePayload `json:"payload"`
}

type evSetSettings struct {
	Event   string      `json:"event"`
	Context string      `json:"context"`
	Payload interface{} `json:"payload"`
}

type evSetImagePayload struct {
	Image  string `json:"image"`
	Target int    `json:"target"`
}

type evSetImage struct {
	Event   string            `json:"event"`
	Context string            `json:"context"`
	Payload evSetImagePayload `json:"payload"`
}

type evSetFeedback struct {
	Event   string      `json:"event"`
	Context string      `json:"context"`
	Payload interface{} `json:"payload"`
}

type evSetFeedbackLayoutPayload struct {
	Layout string `json:"layout"`
}

type evSetFeedbackLayout struct {
	Event   string                     `json:"event"`
	Context string                     `json:"context"`
	Payload evSetFeedbackLayoutPayload `json:"payload"`
}

type evSetTriggerDescription struct {
	Event   string      `json:"event"`
	Context string      `json:"context"`
	Payload interface{} `json:"payload"`
}

// EvDidReceiveGlobalSettingsPayload is the payload from didReceiveGlobalSettings event
type EvDidReceiveGlobalSettingsPayload struct {
	Settings *json.RawMessage `json:"settings"`
}

// EvDidReceiveGlobalSettings is the payload from the didReceiveGlobalSettings event
type EvDidReceiveGlobalSettings struct {
	Event   string                            `json:"event"`
	Payload EvDidReceiveGlobalSettingsPayload `json:"payload"`
}
