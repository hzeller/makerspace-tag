package main

import (
	"fmt"
	"log"

	nfc "github.com/clausecker/nfc/v2"
)

var (
	// These settings works with the ACR122U. Your milage may vary with
	// other devices.
	m = nfc.Modulation{Type: nfc.ISO14443a, BaudRate: nfc.Nbr106}
	// Use an empty string to select first device libnfc sees
	devstr = ""
)

// This will detect tags or cards swiped over the reader.
// Blocks until a target is detected and returns its UID.
// Only cares about the first target it sees.
func GetCard(pnd *nfc.Device) ([10]byte, error) {
	for {
		targets, err := pnd.InitiatorListPassiveTargets(m)
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

func main() {
	log.Println("using libnfc", nfc.Version())

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

		if card_id != [10]byte{} {
			// Print card ID as uppercased hex
			log.Printf("card found: %#X", card_id)
		} else {
			log.Println("no card found")
		}
	}
}
