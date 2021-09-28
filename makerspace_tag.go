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
	"path/filepath"
	"sync"
	"time"
)

const (
	WavPlayer = "/usr/bin/aplay"
)

var (
	modulation = nfc.Modulation{Type: nfc.ISO14443a, BaudRate: nfc.Nbr106}
	devstr     = "" // use first device seen.
)

// This will detect tags or cards swiped over the reader.
// Blocks until a target is detected and returns its UID.
// Only cares about the first target it sees.
func GetCard(pnd *nfc.Device, watchdog *WatchDog) ([10]byte, error) {
	for {
		targets, err := pnd.InitiatorListPassiveTargets(modulation)
		watchdog.TriggerAlive()

		if err != nil {
			return [10]byte{}, fmt.Errorf("failed to list nfc targets: %w", err)
		}

		for _, t := range targets {
			if card, ok := t.(*nfc.ISO14443aTarget); ok {
				return card.UID, nil
			}
		}
		if err = pnd.LastError(); err != nil {
			// This might be an unrecoverable USB situation.
			// Just exit, systemd will restart us
			log.Fatalf("There was some issue with the device %v", err)
		}
	}
}

// Timestamped user has all the fields of the regular user, but also contains
// the time when they tagged. Useful to show in the UI.
type TimestampedUser struct {
	User
	Arrival string `json:"tag_time"`
}

type UserArrival struct {
	sync.Mutex
	last_user *TimestampedUser
	cond      *sync.Cond
}

func NewUserArrival() *UserArrival {
	a := &UserArrival{
		last_user: nil,
	}
	a.cond = sync.NewCond(a)
	return a
}
func (u *UserArrival) Post(user *User) {
	u.Lock()
	u.last_user = &TimestampedUser{
		User:    *user,
		Arrival: time.Now().Format("15:04"),
	}
	u.Unlock()
	u.cond.Broadcast()
}
func (u *UserArrival) WaitNext() *TimestampedUser {
	u.Lock()
	defer u.Unlock()
	u.cond.Wait()
	return u.last_user
}
func (u *UserArrival) LastUser() *TimestampedUser {
	u.Lock()
	defer u.Unlock()
	return u.last_user
}

// Talk to the microorb that runs in server mode with microorb -P 9999
func blink(color string, millis int) {
	http.Get("http://127.0.0.1:9999/set?c=" + color)
	time.Sleep(time.Duration(millis) * time.Millisecond)
	http.Get("http://127.0.0.1:9999/set?c=000000")
}

func beep(soundpath string, attention bool) {
	if attention {
		go exec.Command(WavPlayer, soundpath+"/attention.wav").Run()
	} else {
		go exec.Command(WavPlayer, soundpath+"/accept.wav").Run()
	}
}

func log_tag(logdir string, card [10]byte) error {
	now := time.Now()
	f, err := os.OpenFile(logdir+"/log-"+now.Format("2006-01-02")+".csv",
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
	var user *TimestampedUser
	if is_initial {
		user = user_arrival.LastUser()
	} else {
		user = user_arrival.WaitNext()
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
		log.Printf("Update form: invalid rfid '%s'\n", user_rfid)
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
	dataDir := flag.String("data", "/home/pi", "Directory where user-data and tag-log is stored.")
	resourceDir := flag.String("resources", "/home/pi", "Base directory for html-template and jingle wavs.")

	flag.Parse()

	// Data storage
	userStoreFile := filepath.Join(*dataDir, "tag-users.csv")
	logDir := filepath.Join(*dataDir, "tag-log")
	userStoreChangelog := filepath.Join(*dataDir, "changelog-user-updates.log")
	// Make sure we have the log directory available.
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Fatal("Can't create " + logDir)
	}

	// Resources
	soundPath := filepath.Join(*resourceDir, "tagsounds")
	htmlTemplate := filepath.Join(*resourceDir, "template/tagin.html")

	// Storage of known makerspace users
	userstore := NewUserStore(userStoreFile, userStoreChangelog)
	if userstore == nil {
		// Note, first time, one has to provide an empty file.
		log.Fatalln("Can't read userstore " + userStoreFile)
	}

	// Watchdog will exit when we haven't heard from the NFC for a while.
	watchdog := NewWatchDog(3 * time.Second)

	// Open the NFC stuff.
	pnd, err := nfc.Open(devstr)
	if err != nil {
		log.Fatalln("could not open device:", err)
	}
	defer pnd.Close()

	if err := pnd.InitiatorInit(); err != nil {
		log.Fatalln("could not init initiator:", err)
	}
	log.Println("Successfully opened NFC device", pnd, pnd.Connection())

	user_arrival := NewUserArrival()

	http.HandleFunc("/", // Serving the html page.
		func(w http.ResponseWriter, r *http.Request) {
			http_sendResource(htmlTemplate, w)
		})
	http.HandleFunc("/last-user", // Initial HTML page query
		func(w http.ResponseWriter, r *http.Request) {
			handleUserArrival(user_arrival, true, w, r)
		})
	http.HandleFunc("/arrival", // Inform HTML page about new tag-ins
		func(w http.ResponseWriter, r *http.Request) {
			handleUserArrival(user_arrival, false, w, r)
		})
	http.HandleFunc("/update-user", // Updates sent from the HTML frontend
		func(w http.ResponseWriter, r *http.Request) {
			handleUpdateUser(w, r, user_arrival, userstore)
		})
	go http.ListenAndServe(*bindAddress, nil)

	// Main loop: receive tags, do something with it.
	for {
		card_id, err := GetCard(&pnd, watchdog)
		if err != nil {
			log.Printf("failed to get_card", err)
			continue
		}

		if card_id == [10]byte{} { // All zeroes ... ignore.
			continue
		}

		code := fmt.Sprintf("%X", card_id)

		// Indicate if we know the user with sound and light, then
		// send to
		if user := userstore.get_user(code); user != nil {
			beep(soundPath, false)
			go blink("00ff00", 200)
			user_arrival.Post(user)
		} else {
			beep(soundPath, true)
			go blink("ff0000", 2000)
			user = userstore.createEmptyUser(code)
			user_arrival.Post(user)
		}

		// Log all tag activity with time for later evaluation
		if err := log_tag(logDir, card_id); err != nil {
			log.Printf("Can't write to logfile: %v\n", err)
		}
	}
}
