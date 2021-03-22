package logging

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/jitsucom/jitsu/server/safego"
	"io"
)

//AsyncLogger write json logs to file system in different goroutine
type AsyncLogger struct {
	writer             io.WriteCloser
	logCh              chan interface{}
	showInGlobalLogger bool

	closed bool
}

//Create AsyncLogger and run goroutine that's read from channel and write to file
func NewAsyncLogger(writer io.WriteCloser, showInGlobalLogger bool) *AsyncLogger {
	logger := &AsyncLogger{writer: writer, logCh: make(chan interface{}, 20000), showInGlobalLogger: showInGlobalLogger}

	safego.RunWithRestart(func() {
		for {
			if logger.closed {
				break
			}

			event := <-logger.logCh
			bts, err := json.Marshal(event)
			if err != nil {
				Errorf("Error marshaling event to json: %v", err)
				continue
			}

			if logger.showInGlobalLogger {
				prettyJsonBytes, _ := json.MarshalIndent(&event, " ", " ")
				Info(string(prettyJsonBytes))
			}

			buf := bytes.NewBuffer(bts)
			buf.Write([]byte("\n"))

			if _, err := logger.writer.Write(buf.Bytes()); err != nil {
				Errorf("Error writing event to log file: %v", err)
				continue
			}
		}
	})

	return logger
}

//Consume event event and put it to channel
func (al *AsyncLogger) Consume(event map[string]interface{}, tokenId string) {
	al.logCh <- event
}

//ConsumeAny put interface{} to the channel
func (al *AsyncLogger) ConsumeAny(object interface{}) {
	al.logCh <- object
}

//Close underlying log file writer
func (al *AsyncLogger) Close() (resultErr error) {
	al.closed = true

	if err := al.writer.Close(); err != nil {
		return fmt.Errorf("Error closing writer: %v", err)
	}
	return nil
}
