package ipc

import (
	"bufio"
	"encoding/json"
	"io"
	"sync"
)

// RequestFrame is one NDJSON command frame sent from NSH TSX to Go core.
type RequestFrame struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ResponseError is a structured error returned in ResponseFrame.
type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ResponseFrame is one NDJSON reply frame correlated by ID.
type ResponseFrame struct {
	ID     string         `json:"id"`
	Result any            `json:"result,omitempty"`
	Error  *ResponseError `json:"error,omitempty"`
}

// EventFrame is an asynchronous push notification emitted by Go core to NSH TSX.
type EventFrame struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// TransportWriter serializes concurrent NDJSON writes to stdout.
type TransportWriter struct {
	mu  sync.Mutex
	out io.Writer
}

// NewTransportWriter wraps an output writer (typically os.Stdout).
func NewTransportWriter(out io.Writer) *TransportWriter {
	return &TransportWriter{out: out}
}

// WriteResponse writes one ResponseFrame line.
func (w *TransportWriter) WriteResponse(resp ResponseFrame) error {
	return w.writeLine(resp)
}

// WriteEvent writes one EventFrame line.
func (w *TransportWriter) WriteEvent(event string, data any) error {
	return w.writeLine(EventFrame{Event: event, Data: data})
}

func (w *TransportWriter) writeLine(v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := w.out.Write(append(raw, '\n')); err != nil {
		return err
	}
	return nil
}

// ReadRequestFrames reads NDJSON RequestFrames from in until EOF, calling onFrame for each.
func ReadRequestFrames(in io.Reader, onFrame func(RequestFrame)) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var frame RequestFrame
		if err := json.Unmarshal(line, &frame); err != nil {
			continue
		}
		onFrame(frame)
	}
	return scanner.Err()
}
