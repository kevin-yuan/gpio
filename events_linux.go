package gpio

import (
	"fmt"
	"io"
	"syscall"
	"time"
	"unsafe"

	"github.com/mkch/gpio/internal/fdevents"
	"github.com/mkch/gpio/internal/sys"
	"golang.org/x/sys/unix"
)

// LineWithEvent is an opened GPIO line whose events can be subscribed.
type LineWithEvent struct {
	l      Line
	events *fdevents.FdEvents
}

func (l *LineWithEvent) Close() (err error) {
	err1 := l.l.Close()
	err2 := l.events.Close()
	if err1 != nil {
		return err1
	}
	return err2

}

// Value returns the current value of the GPIO line. 1 (high) or 0 (low).
func (l *LineWithEvent) Value() (value byte, err error) {
	return l.l.Value()
}

func readGPIOLineEventFd(fd int) time.Time {
	var eventData sys.GPIOEventData
	_, err := io.ReadFull(sys.FdReader(fd), (*[unsafe.Sizeof(eventData)]byte)(unsafe.Pointer(&eventData))[:])
	if err != nil {
		if err == syscall.EINTR {
			return time.Now() // Hack here.
		}
		panic(fmt.Errorf("failed to read GPIO event: %w", err))
	}

	sec := uint64(time.Nanosecond) * eventData.Timestamp / uint64(time.Second)
	nano := uint64(time.Nanosecond) * eventData.Timestamp % uint64(time.Second)
	return time.Unix(int64(sec), int64(nano))
}

func newInputLineWithEvents(chipFd int, offset uint32, flags, eventFlags uint32, consumer string) (line *LineWithEvent, err error) {
	var req = sys.GPIOEventRequest{
		LineOffset:  offset,
		HandleFlags: uint32(flags),
		EventFlags:  uint32(eventFlags)}
	copy(req.ConsumerLabel[:], consumer)
	err = sys.Ioctl(chipFd, sys.GPIO_GET_LINEEVENT_IOCTL, uintptr(unsafe.Pointer(&req)))
	if err != nil {
		err = fmt.Errorf("request GPIO event failed: ioctl %w", err)
		return
	}
	events, err := fdevents.New(int(req.Fd), unix.EPOLLIN|unix.EPOLLPRI|unix.EPOLLET, readGPIOLineEventFd)
	if err != nil {
		unix.Close(int(req.Fd))
		return
	}

	fd2, err := unix.Dup(int(req.Fd))
	if err != nil {
		err = fmt.Errorf("request GPIO event failed: dup %w", err)
		return
	}
	line = &LineWithEvent{
		l:      Line{fd: fd2, numLines: 1},
		events: events,
	}
	return
}

// Events returns an channel from which the occurrence time of GPIO events can be read.
// The best estimate of time of event occurrence is sent to the returned channel,
// and the channel is closed when l is closed.
//
// Package gpio will not block sending to the channel: it only keeps the lastest
// value in the channel.
func (l *LineWithEvent) Events() <-chan time.Time {
	return l.events.Events()
}
