package connect

import (
	"testing"
	"time"
)

func TestDispatchTextRunsEventHandlerAsync(t *testing.T) {
	h := GetHandlers()
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	t.Cleanup(func() {
		h.SetEventHandler(nil)
	})

	h.SetEventHandler(func(Event) error {
		close(started)
		<-release
		return nil
	})

	data, err := EncodeEvent("instruction", nil)
	if err != nil {
		t.Fatalf("encode event: %v", err)
	}

	dispatchDone := make(chan error, 1)
	go func() {
		dispatchDone <- h.DispatchText(data, func([]byte) error { return nil })
	}()

	select {
	case <-started:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("event handler did not start")
	}

	select {
	case err := <-dispatchDone:
		if err != nil {
			t.Fatalf("dispatch text: %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("DispatchText blocked on event handler")
	}
}
