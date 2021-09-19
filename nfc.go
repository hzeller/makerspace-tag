package main

import (
	"fmt"
	nfc "github.com/clausecker/nfc/v2"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const (
	WavPlayer = "/usr/bin/aplay"
	SoundPath = "/home/pi/tagsounds"
	LogDir    = "/home/pi/tag-log"
)

var (
	modulation = nfc.Modulation{Type: nfc.ISO14443a, BaudRate: nfc.Nbr106}
	devstr     = "" // use first device seen.
)

// This will detect tags or cards swiped over the reader.
// Blocks until a target is detected and returns its UID.
// Only cares about the first target it sees.
func GetCard(pnd *nfc.Device) ([10]byte, error) {
	for {
		targets, err := pnd.InitiatorListPassiveTargets(modulation)
		if err != nil {
			return [10]byte{}, fmt.Errorf("failed to list nfc targets: %w", err)
		}

		for _, t := range targets {
			if card, ok := t.(*nfc.ISO14443aTarget); ok {
				return card.UID, nil
			}
		}
	}
}

func blink(color string, millis int) {
	http.Get("http://127.0.0.1:9999/set?c=" + color)
	time.Sleep(time.Duration(millis) * time.Millisecond)
	http.Get("http://127.0.0.1:9999/set?c=000000")
}

func beep(issue bool) {
	if issue {
		go exec.Command(WavPlayer, SoundPath+"/attention.wav").Run()
	} else {
		go exec.Command(WavPlayer, SoundPath+"/accept.wav").Run()
	}
}

func has_access(card [10]byte) bool {
	return card == [10]byte{0xA2, 0x36, 0x3D, 0x55, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

func log_tag(card [10]byte) error {
	now := time.Now()
	f, err := os.OpenFile(LogDir+"/log-"+now.Format("2006-01-02")+".csv",
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	fmt.Fprintf(f, "%s,%X\n", time.Now().Format("2006-01-02 15:04:05"), card)
	f.Close()
	return nil
}

func main() {
	pnd, err := nfc.Open(devstr)
	if err != nil {
		log.Fatalln("could not open device:", err)
	}
	defer pnd.Close()

	if err := pnd.InitiatorInit(); err != nil {
		log.Fatalln("could not init initiator:", err)
	}

	log.Println("opened device", pnd, pnd.Connection())

	for {
		card_id, err := GetCard(&pnd)
		if err != nil {
			log.Printf("failed to get_card", err)
			continue
		}

		if card_id == [10]byte{} { // All zeroes ... ignore.
			continue
		}

		if has_access(card_id) {
			beep(false)
			go blink("00ff00", 200)
		} else {
			beep(true)
			go blink("ff0000", 2000)
		}
		if err := log_tag(card_id); err != nil {
			log.Printf("Can't write to logfile: %v\n", err)
		}
	}
}
