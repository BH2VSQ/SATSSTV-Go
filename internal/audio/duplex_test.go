package audio

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeCapture struct {
	mu        sync.Mutex
	callback  func([]float32)
	startCnt  int
	stopCnt   int
	startedCh chan struct{}
}

func newFakeCapture() *fakeCapture { return &fakeCapture{startedCh: make(chan struct{}, 1)} }
func (f *fakeCapture) StartCapture(_ context.Context, cb func([]float32)) error {
	f.mu.Lock()
	f.callback = cb
	f.startCnt++
	f.mu.Unlock()
	f.startedCh <- struct{}{}
	return nil
}
func (f *fakeCapture) StopCapture() error {
	f.mu.Lock()
	f.stopCnt++
	f.mu.Unlock()
	return nil
}
func (f *fakeCapture) Emit(samples []float32) {
	f.mu.Lock()
	cb := f.callback
	f.mu.Unlock()
	if cb != nil {
		cb(samples)
	}
}
func (f *fakeCapture) Counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.startCnt, f.stopCnt
}

type fakePlayback struct {
	started chan struct{}
	finish  chan struct{}
	err     error
}

func newFakePlayback() *fakePlayback {
	return &fakePlayback{started: make(chan struct{}, 1), finish: make(chan struct{})}
}
func (f *fakePlayback) Play(ctx context.Context, _ []float32, _ int) error {
	f.started <- struct{}{}
	select {
	case <-f.finish:
		return f.err
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *fakePlayback) StopPlayback() error { return nil }

type fakePTT struct {
	mu         sync.Mutex
	states     []bool
	releaseErr error
}

func (f *fakePTT) SetPTT(_ context.Context, enabled bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.states = append(f.states, enabled)
	if !enabled {
		return f.releaseErr
	}
	return nil
}
func (f *fakePTT) Close() error { return nil }
func (f *fakePTT) States() []bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]bool(nil), f.states...)
}

type fakeVOXPTT struct{ fakePTT }

func (*fakeVOXPTT) RequiresPTTLeadTime() bool { return false }

func TestVOXDoesNotWaitForHardwarePTTLead(t *testing.T) {
	capture := newFakeCapture()
	playback := newFakePlayback()
	vox := &fakeVOXPTT{}
	duplex, err := NewFullDuplex(capture, playback, vox, 48_000, TxTiming{PTTLead: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()
	done := make(chan error, 1)
	started := time.Now()
	go func() { done <- duplex.Transmit(context.Background(), []float32{0.1}) }()
	select {
	case <-playback.started:
		if elapsed := time.Since(started); elapsed > 120*time.Millisecond {
			t.Fatalf("VOX playback started after %s; the 300 ms hardware lead should be skipped", elapsed)
		}
	case <-time.After(120 * time.Millisecond):
		t.Fatal("VOX playback did not start promptly")
	}
	close(playback.finish)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("VOX transmission did not finish")
	}
}

func TestHardwarePTTStillHonorsConfiguredLead(t *testing.T) {
	capture := newFakeCapture()
	playback := newFakePlayback()
	radio := &fakePTT{}
	duplex, err := NewFullDuplex(capture, playback, radio, 48_000, TxTiming{PTTLead: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()
	started := time.Now()
	done := make(chan error, 1)
	go func() { done <- duplex.Transmit(context.Background(), []float32{0.1}) }()
	select {
	case <-playback.started:
		t.Fatal("hardware PTT playback began before its configured lead interval")
	case <-time.After(40 * time.Millisecond):
	}
	select {
	case <-playback.started:
		if elapsed := time.Since(started); elapsed < 80*time.Millisecond {
			t.Fatalf("hardware PTT playback began after %s, expected ~100 ms lead", elapsed)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("hardware PTT playback did not start after lead interval")
	}
	close(playback.finish)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("hardware PTT transmission did not finish")
	}
}

func TestReceiveContinuesDuringTransmit(t *testing.T) {
	capture := newFakeCapture()
	playback := newFakePlayback()
	radio := &fakePTT{}
	duplex, err := NewFullDuplex(capture, playback, radio, 48_000, TxTiming{})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()
	received := make(chan struct{}, 1)
	if err := duplex.StartReceive(context.Background(), func(samples []float32) {
		if len(samples) > 0 {
			received <- struct{}{}
		}
	}); err != nil {
		t.Fatal(err)
	}
	txDone := make(chan error, 1)
	go func() { txDone <- duplex.Transmit(context.Background(), []float32{0.1, 0.2}) }()
	select {
	case <-playback.started:
	case <-time.After(time.Second):
		t.Fatal("TX playback did not start")
	}
	state := duplex.State()
	if !state.RXActive || !state.TXActive {
		t.Fatalf("expected RX and TX active concurrently, got %+v", state)
	}
	capture.Emit([]float32{0.1, 0.2, 0.3})
	select {
	case <-received:
	case <-time.After(time.Second):
		t.Fatal("RX callback was blocked while TX was active")
	}
	close(playback.finish)
	select {
	case err := <-txDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("TX did not finish")
	}
	if got := radio.States(); len(got) != 2 || !got[0] || got[1] {
		t.Fatalf("PTT states = %v, want [true false]", got)
	}
	if starts, stops := capture.Counts(); starts != 1 || stops != 0 {
		t.Fatalf("capture counts during TX = %d/%d, want 1/0", starts, stops)
	}
}

func TestTransmitReleasesPTTWhenPlaybackFails(t *testing.T) {
	capture := newFakeCapture()
	playback := newFakePlayback()
	playback.err = errors.New("device lost")
	radio := &fakePTT{}
	duplex, err := NewFullDuplex(capture, playback, radio, 48_000, TxTiming{})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()
	close(playback.finish)
	if err := duplex.Transmit(context.Background(), []float32{0.1}); err == nil {
		t.Fatal("expected playback error")
	}
	states := radio.States()
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("PTT states = %v, want asserted then released", states)
	}
}

func TestTransmitCancellationReleasesPTT(t *testing.T) {
	capture := newFakeCapture()
	playback := newFakePlayback()
	radio := &fakePTT{}
	duplex, err := NewFullDuplex(capture, playback, radio, 48_000, TxTiming{})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()

	txCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	txDone := make(chan error, 1)
	go func() { txDone <- duplex.Transmit(txCtx, make([]float32, 48_000)) }()
	select {
	case <-playback.started:
	case <-time.After(time.Second):
		t.Fatal("TX playback did not start")
	}
	cancel()
	select {
	case err := <-txDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("transmit error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("TX did not stop after context cancellation")
	}
	states := radio.States()
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("PTT states = %v, want asserted then released", states)
	}
}

func TestTransmitBusyRejectsConcurrentTX(t *testing.T) {
	capture := newFakeCapture()
	playback := newFakePlayback()
	radio := &fakePTT{}
	duplex, err := NewFullDuplex(capture, playback, radio, 48_000, TxTiming{})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()
	firstDone := make(chan error, 1)
	go func() { firstDone <- duplex.Transmit(context.Background(), []float32{0.1}) }()
	select {
	case <-playback.started:
	case <-time.After(time.Second):
		t.Fatal("first TX did not start")
	}
	if err := duplex.Transmit(context.Background(), []float32{0.2}); !errors.Is(err, ErrTransmitBusy) {
		t.Fatalf("second TX error = %v, want ErrTransmitBusy", err)
	}
	close(playback.finish)
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("first TX did not finish")
	}
}

func TestReceiveContextCancellationStopsCapture(t *testing.T) {
	capture := newFakeCapture()
	playback := newFakePlayback()
	radio := &fakePTT{}
	duplex, err := NewFullDuplex(capture, playback, radio, 48_000, TxTiming{})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()

	rxCtx, cancel := context.WithCancel(context.Background())
	if err := duplex.StartReceive(rxCtx, func([]float32) {}); err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !duplex.State().RXActive {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if duplex.State().RXActive {
		t.Fatal("RX state remained active after its context was cancelled")
	}
	if starts, stops := capture.Counts(); starts != 1 || stops != 1 {
		t.Fatalf("capture counts = %d/%d, want 1/1", starts, stops)
	}
}

func TestTransmitWatchdogReleasesPTT(t *testing.T) {
	capture := newFakeCapture()
	playback := newFakePlayback()
	radio := &fakePTT{}
	duplex, err := NewFullDuplex(capture, playback, radio, 48_000, TxTiming{
		MaxTransmitDuration: 30 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer duplex.Close()

	err = duplex.Transmit(context.Background(), make([]float32, 48_000))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("transmit error = %v, want watchdog deadline", err)
	}
	states := radio.States()
	if len(states) != 2 || !states[0] || states[1] {
		t.Fatalf("PTT states = %v, want [true false]", states)
	}
}

func TestEnqueueCaptureBlockReplacesStaleBacklogWithBoundary(t *testing.T) {
	queue := make(chan []float32, 2)
	queue <- []float32{1}
	queue <- []float32{2}
	if dropped := enqueueCaptureBlock(queue, []float32{3}); dropped != 2 {
		t.Fatalf("discarded %d blocks, want 2", dropped)
	}
	marker := <-queue
	if marker != nil {
		t.Fatalf("first queued item = %v, want discontinuity marker", marker)
	}
	latest := <-queue
	if len(latest) != 1 || latest[0] != 3 {
		t.Fatalf("latest block = %v, want [3]", latest)
	}
}

func TestEnqueueCaptureBlockPreservesSamplesWithoutOverflow(t *testing.T) {
	queue := make(chan []float32, 2)
	if dropped := enqueueCaptureBlock(queue, []float32{1}); dropped != 0 {
		t.Fatalf("discarded %d blocks without overflow, want 0", dropped)
	}
	if dropped := enqueueCaptureBlock(queue, []float32{2}); dropped != 0 {
		t.Fatalf("discarded %d blocks below capacity, want 0", dropped)
	}
	for want := float32(1); want <= 2; want++ {
		got := <-queue
		if len(got) != 1 || got[0] != want {
			t.Fatalf("queued block = %v, want [%v]", got, want)
		}
	}
}
