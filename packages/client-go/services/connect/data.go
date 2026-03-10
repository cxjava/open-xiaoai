package connect

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// AppMessage is the top-level tagged enum for WebSocket text messages.
// Wire format: {"Request": {...}} | {"Response": {...}} | {"Event": {...}} | {"Stream": {...}}
type AppMessage struct {
	Request  *Request  `json:"Request,omitempty"`
	Response *Response `json:"Response,omitempty"`
	Event    *Event    `json:"Event,omitempty"`
	Stream   *Stream   `json:"Stream,omitempty"`
}

func (m *AppMessage) Type() string {
	switch {
	case m.Request != nil:
		return "Request"
	case m.Response != nil:
		return "Response"
	case m.Event != nil:
		return "Event"
	case m.Stream != nil:
		return "Stream"
	default:
		return "Unknown"
	}
}

type Request struct {
	ID      string           `json:"id"`
	Command string           `json:"command"`
	Payload *json.RawMessage `json:"payload,omitempty"`
}

type Response struct {
	ID   string           `json:"id"`
	Code *int             `json:"code,omitempty"`
	Msg  *string          `json:"msg,omitempty"`
	Data *json.RawMessage `json:"data,omitempty"`
}

func SuccessResponse() Response {
	code := 0
	msg := "success"
	return Response{ID: "0", Code: &code, Msg: &msg}
}

func DataResponse(data json.RawMessage) Response {
	return Response{ID: "0", Data: &data}
}

func ErrorResponse(id string, err error) Response {
	code := -1
	msg := err.Error()
	return Response{ID: id, Code: &code, Msg: &msg}
}

type Event struct {
	ID    string           `json:"id"`
	Event string           `json:"event"`
	Data  *json.RawMessage `json:"data,omitempty"`
}

func NewEvent(event string, data *json.RawMessage) Event {
	return Event{
		ID:    uuid.NewString(),
		Event: event,
		Data:  data,
	}
}

type Stream struct {
	ID    string           `json:"id"`
	Tag   string           `json:"tag"`
	Bytes []byte           `json:"bytes"`
	Data  *json.RawMessage `json:"data,omitempty"`
}

func NewStream(tag string, bytes []byte, data *json.RawMessage) Stream {
	return Stream{
		ID:    uuid.NewString(),
		Tag:   tag,
		Bytes: bytes,
		Data:  data,
	}
}

// MarshalAppMessage wraps the inner type into the tagged envelope.
func MarshalRequest(r Request) ([]byte, error) {
	return json.Marshal(AppMessage{Request: &r})
}

func MarshalResponse(r Response) ([]byte, error) {
	return json.Marshal(AppMessage{Response: &r})
}

func MarshalEvent(e Event) ([]byte, error) {
	return json.Marshal(AppMessage{Event: &e})
}

// ParseTextMessage parses a JSON text message into an AppMessage.
func ParseTextMessage(data []byte) (*AppMessage, error) {
	var msg AppMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("parse text message: %w", err)
	}
	return &msg, nil
}

// ParseStreamMessage parses a binary message (JSON-encoded Stream).
func ParseStreamMessage(data []byte) (*Stream, error) {
	var s Stream
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse stream message: %w", err)
	}
	return &s, nil
}
