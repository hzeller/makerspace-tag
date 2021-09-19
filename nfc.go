package main

import (
	"encoding/json"
	"flag"
	"fmt"
	nfc "github.com/clausecker/nfc/v2"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const (
	WavPlayer     = "/usr/bin/aplay"
	BaseDir       = "/home/pi"
	SoundPath     = BaseDir + "/tagsounds"
	LogDir        = BaseDir + "/tag-log"
	UserStoreFile = BaseDir + "/tag-users.csv"

	MainPageTemplate = BaseDir + "/template/tagin.html"
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

func http_sendResource(local_path string, out http.ResponseWriter) {
	cache_time := 10 // Should be large once we are done.
	header_addon := ""
	content, _ := ioutil.ReadFile(local_path)
	if content == nil {
		cache_time = 10 // fallbacks might change more often.
		out.WriteHeader(http.StatusNotFound)
		header_addon = ",must-revalidate"
	}
	out.Header().Set("Cache-Control", fmt.Sprintf("max-age=%d%s", cache_time, header_addon))
	out.Header().Set("Content-Type", "text/html; charset=utf-8")
	out.Write(content)
}

func HandleUserArrival(user_channel chan *User) {
	for {
		u := <-user_channel
		log.Printf("Channel: %s\n", u.Name)
	}
}
func main() {
	bindAddress := flag.String("bind-address", "localhost:2000", "Port to serve from")
	flag.Parse()

	pnd, err := nfc.Open(devstr)
	if err != nil {
		log.Fatalln("could not open device:", err)
	}
	defer pnd.Close()

	userstore := NewUserStore(UserStoreFile)
	if userstore == nil {
		log.Fatalln("Can't read userstore " + UserStoreFile)
	}

	if err := pnd.InitiatorInit(); err != nil {
		log.Fatalln("could not init initiator:", err)
	}

	log.Println("opened device", pnd, pnd.Connection())

	user_channel := make(chan *User)
	go HandleUserArrival(user_channel)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http_sendResource(MainPageTemplate, w)
	})
	go http.ListenAndServe(*bindAddress, nil)

	for {
		card_id, err := GetCard(&pnd)
		if err != nil {
			log.Printf("failed to get_card", err)
			continue
		}

		if card_id == [10]byte{} { // All zeroes ... ignore.
			continue
		}

		code := fmt.Sprintf("%X", card_id)

		if user := userstore.get_user(code); user != nil {
			beep(false)
			go blink("00ff00", 200)
			json, _ := json.Marshal(user)
			log.Printf("Got user %s\n", json)
			user_channel <- user
		} else {
			beep(true)
			go blink("ff0000", 2000)
			log.Printf("Unknown user.\n")
			user = userstore.createEmptyUser(code)
			user_channel <- user
		}
		if err := log_tag(card_id); err != nil {
			log.Printf("Can't write to logfile: %v\n", err)
		}
	}
}
