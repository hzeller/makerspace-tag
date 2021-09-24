package main

import (
	"log"
	"time"
)

type WatchDog struct {
	active chan bool
}

func NewWatchDog(timeout time.Duration) *WatchDog {
	w := &WatchDog{
		active: make(chan bool, 5),
	}
	go func() {
		for {
			select {
			case <-w.active:
			case <-time.After(timeout):
				log.Fatalf("Watchdog reached timeout.")
			}
		}
	}()
	return w
}
func (w *WatchDog) TriggerAlive() {
	w.active <- true
}
