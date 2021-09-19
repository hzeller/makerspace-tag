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

type UserArrival struct {
	user_channel chan *User
	last_user    *User
}

func NewUserArrival() *UserArrival {
	return &UserArrival{
		user_channel: make(chan *User),
	}
}
func (u *UserArrival) Post(user *User) {
	u.user_channel <- user
	u.last_user = user
}
func (u *UserArrival) Receive() *User {
	user := <-u.user_channel
	return user
}
func (u *UserArrival) LastUser() *User {
	return u.last_user
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

func handleUserArrival(user_arrival *UserArrival, is_initial bool, w http.ResponseWriter, r *http.Request) {
	var user *User
	if is_initial {
		user = user_arrival.LastUser()
	} else {
		user = user_arrival.Receive()
	}
	w.Header().Set("Conent-Type", "application/json")
	if user != nil {
		json, _ := json.Marshal(user)
		w.Write(json)
	} else {
		fmt.Fprintf(w, "{}")
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func handleUpdateUser(w http.ResponseWriter, r *http.Request, post_result *UserArrival, userstore *UserStore) {
	defer http.Redirect(w, r, "/", http.StatusSeeOther)

	if err := r.ParseForm(); err != nil {
		log.Printf("ParseForm() err: %v", err)
		return
	}
	user_rfid := r.FormValue("user_rfid")
	if len(user_rfid) != 20 { // super-simplistic validation.
		log.Printf("Update form: invalid rfid %s\n", user_rfid)
		return
	}
	userstore.InsertOrUpdateUser(user_rfid, func(user *User) bool {
		user.UpdateFromFormValues(r)
		post_result.Post(user)
		return true
	})
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

	user_arrival := NewUserArrival()

	http.HandleFunc("/arrival", func(w http.ResponseWriter, r *http.Request) {
		handleUserArrival(user_arrival, false, w, r)
	})
	http.HandleFunc("/last-user", func(w http.ResponseWriter, r *http.Request) {
		handleUserArrival(user_arrival, true, w, r)
	})

	http.HandleFunc("/update-user", func(w http.ResponseWriter, r *http.Request) {
		handleUpdateUser(w, r, user_arrival, userstore)
	})
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
			user_arrival.Post(user)
		} else {
			beep(true)
			go blink("ff0000", 2000)
			user = userstore.createEmptyUser(code)
			user_arrival.Post(user)
		}
		if err := log_tag(card_id); err != nil {
			log.Printf("Can't write to logfile: %v\n", err)
		}
	}
}